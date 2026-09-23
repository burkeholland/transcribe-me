package transcribe

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fakeAssets(t *testing.T) (string, []byte) {
	t.Helper()
	root := t.TempDir()
	m := Manifest{}
	data := []byte("fixture")
	sum := sha256.Sum256(data)
	for _, name := range []string{
		"runtime/whisper/whisper-cli.exe", "runtime/ffmpeg/bin/ffmpeg.exe", "runtime/ffmpeg/bin/ffprobe.exe",
		"runtime/models/ggml-base.bin", "runtime/models/ggml-silero-v5.1.2.bin",
	} {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		asset := Asset{Path: name, Size: int64(len(data)), SHA256: hex.EncodeToString(sum[:])}
		if strings.Contains(name, "/models/") {
			asset.URL = "https://huggingface.co/example/models/resolve/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/" + filepath.Base(name)
		}
		m.Files = append(m.Files, asset)
	}
	manifest, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return root, manifest
}

func copyAssets(t *testing.T, source, target string, manifest Manifest, selected map[string]bool) {
	t.Helper()
	for _, asset := range selectAssets(manifest, selected) {
		data, err := os.ReadFile(filepath.Join(source, filepath.FromSlash(asset.Path)))
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(target, filepath.FromSlash(asset.Path))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func readyService(t *testing.T, runner Runner) *Service {
	t.Helper()
	root, manifest := fakeAssets(t)
	s := NewService(root, t.TempDir(), runner)
	if err := s.Initialize(context.Background(), manifest); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

func waitService(t *testing.T, s *Service) Snapshot {
	t.Helper()
	s.mu.Lock()
	done := s.done
	s.mu.Unlock()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("job did not finish")
	}
	return s.Status()
}

func TestServiceLifecycle(t *testing.T) {
	s := readyService(t, fakeRunner)
	if err := s.Start(context.Background(), testMedia(t), "auto"); err != nil {
		t.Fatal(err)
	}
	status := waitService(t, s)
	if status.Job.State != "completed" || status.Job.Progress != 100 || len(status.History) != 1 {
		t.Fatalf("%+v %+v", status, status.Job)
	}
	transcript, err := s.Get(status.Job.TranscriptID)
	if err != nil || transcript.WordCount != 5 {
		t.Fatalf("%+v %v", transcript, err)
	}
	entries, err := os.ReadDir(filepath.Join(s.dataDir, "work"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("temporary audio remained: %+v %v", entries, err)
	}
	status.History[0].FileName = "mutated"
	status.Job.State = "mutated"
	if s.Status().Job.State != "completed" || s.Status().History[0].FileName == "mutated" {
		t.Fatal("snapshot exposed shared mutable state")
	}
	if err := s.Cancel(); err == nil {
		t.Fatal("cancellation accepted with no active job")
	}
}

func TestCancelAndBusy(t *testing.T) {
	started := make(chan struct{})
	s := readyService(t, func(ctx context.Context, exe string, args []string, cb func(string)) ([]byte, error) {
		if filepath.Base(exe) == "ffmpeg.exe" {
			close(started)
			<-ctx.Done()
			return nil, ctx.Err()
		}
		return fakeRunner(ctx, exe, args, cb)
	})
	path := testMedia(t)
	if err := s.Start(context.Background(), path, "en"); err != nil {
		t.Fatal(err)
	}
	<-started
	if err := s.Start(context.Background(), path, "en"); err == nil {
		t.Fatal("concurrent job accepted")
	}
	if err := s.Cancel(); err != nil {
		t.Fatal(err)
	}
	status := waitService(t, s)
	if status.Job.State != "cancelled" || len(status.History) != 0 {
		t.Fatalf("%+v %+v", status, status.Job)
	}
	entries, err := os.ReadDir(filepath.Join(s.dataDir, "work"))
	if err != nil || len(entries) != 0 {
		t.Fatal("cancelled audio was not removed")
	}
}

func TestServiceFailures(t *testing.T) {
	s := readyService(t, fakeRunner)
	if err := s.Start(context.Background(), "anything", "--bad-language"); err == nil {
		t.Fatal("unsupported language accepted")
	}
	if err := s.Start(context.Background(), filepath.Join(t.TempDir(), "gone.mp4"), "en"); err != nil {
		t.Fatal(err)
	}
	if status := waitService(t, s); status.Job.State != "failed" || status.Job.Error == "" {
		t.Fatal("missing media produced success")
	}
}

func TestInterruptedAudioCleanup(t *testing.T) {
	work := t.TempDir()
	job := filepath.Join(work, strings.Repeat("c", 32))
	if err := os.Mkdir(job, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(job, "audio.wav"), []byte("private audio"), 0600); err != nil {
		t.Fatal(err)
	}
	unrelated := filepath.Join(work, "not-an-app-job")
	if err := os.Mkdir(unrelated, 0700); err != nil {
		t.Fatal(err)
	}
	if err := cleanInterruptedJobs(work); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(job); !os.IsNotExist(err) {
		t.Fatal("interrupted job audio remains")
	}
	if _, err := os.Stat(unrelated); err != nil {
		t.Fatal("unrelated directory was removed")
	}
}

func TestAssetVerification(t *testing.T) {
	root, manifestData := fakeAssets(t)
	manifest, err := ParseManifest(manifestData)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyManifest(context.Background(), root, manifest); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "runtime", "models", "ggml-base.bin"), []byte("damaged"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := VerifyManifest(context.Background(), root, manifest); err == nil || !strings.Contains(err.Error(), "SHA-256") {
		t.Fatalf("expected hash rejection: %v", err)
	}
	s := NewService(root, t.TempDir(), fakeRunner)
	if err := s.Initialize(context.Background(), manifestData); err != nil {
		t.Fatal(err)
	}
	status := s.Status()
	if status.Ready || status.RuntimeState != "required" || status.SetupError != "" {
		t.Fatalf("damaged runtime did not request repair: %+v", status)
	}
	for _, data := range []string{`{`, `{"files":[]}`, `{"files":[{"path":"../outside"}]}`} {
		if _, err := ParseManifest([]byte(data)); err == nil {
			t.Fatalf("accepted invalid manifest %s", data)
		}
	}
}

func TestParseManifestRejectsInvalidSHA256(t *testing.T) {
	_, manifestData := fakeAssets(t)
	valid := mustManifest(t, manifestData)
	for _, test := range []struct {
		name string
		hash string
	}{
		{name: "malformed", hash: strings.Repeat("g", sha256.Size*2)},
		{name: "uppercase", hash: strings.Repeat("A", sha256.Size*2)},
	} {
		t.Run(test.name, func(t *testing.T) {
			manifest := Manifest{Files: append([]Asset(nil), valid.Files...)}
			manifest.Files[0].SHA256 = test.hash
			data, err := json.Marshal(manifest)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := ParseManifest(data); err == nil {
				t.Fatalf("accepted %s SHA-256 %q", test.name, test.hash)
			}
		})
	}
}

func TestParseManifestRequiresPinnedModelSources(t *testing.T) {
	_, manifestData := fakeAssets(t)
	valid := mustManifest(t, manifestData)
	for _, test := range []struct {
		name string
		edit func([]Asset)
	}{
		{
			name: "moving revision",
			edit: func(files []Asset) {
				for index := range files {
					if files[index].URL != "" {
						files[index].URL = strings.Replace(files[index].URL, strings.Repeat("a", 40), "main", 1)
						return
					}
				}
			},
		},
		{
			name: "untrusted host",
			edit: func(files []Asset) {
				for index := range files {
					if files[index].URL != "" {
						files[index].URL = strings.Replace(files[index].URL, "huggingface.co", "example.com", 1)
						return
					}
				}
			},
		},
		{
			name: "native asset URL",
			edit: func(files []Asset) {
				files[0].URL = "https://huggingface.co/example/models/resolve/" + strings.Repeat("a", 40) + "/" + filepath.Base(files[0].Path)
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			files := append([]Asset(nil), valid.Files...)
			test.edit(files)
			data, err := json.Marshal(Manifest{Files: files})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := ParseManifest(data); err == nil {
				t.Fatalf("accepted %s model source", test.name)
			}
		})
	}
}

// A damaged runtime must not hide the user's saved transcripts. The user should
// still be able to review and export them from the read-only history view.
func TestHistoryStillLoadsWhenRuntimeIsBroken(t *testing.T) {
	root, manifest := fakeAssets(t)
	if err := os.WriteFile(filepath.Join(root, "runtime", "models", "ggml-base.bin"), []byte("damaged"), 0600); err != nil {
		t.Fatal(err)
	}
	dataDir := t.TempDir()
	storeDir := filepath.Join(dataDir, "transcripts")
	if err := os.MkdirAll(storeDir, 0700); err != nil {
		t.Fatal(err)
	}
	saved := exampleTranscript()
	if err := (Store{Dir: storeDir}).Save(saved); err != nil {
		t.Fatal(err)
	}
	s := NewService(root, dataDir, fakeRunner)
	if err := s.Initialize(context.Background(), manifest); err != nil {
		t.Fatal(err)
	}
	status := s.Status()
	if status.Ready {
		t.Fatal("service reported ready with a damaged runtime")
	}
	if status.RuntimeState != "required" || status.RuntimeError != "" {
		t.Fatalf("runtime repair was not offered: %+v", status)
	}

	if len(status.History) != 1 || status.History[0].ID != saved.ID {
		t.Fatalf("history was hidden by the damaged runtime: %+v", status.History)
	}
	got, err := s.Get(saved.ID)
	if err != nil || got.Text != saved.Text {
		t.Fatalf("Get failed while runtime was broken: %+v err=%v", got, err)
	}
	if _, err := Format(got, "txt"); err != nil {
		t.Fatalf("Export failed while runtime was broken: %v", err)
	}
	if err := s.Start(context.Background(), "anything.mp4", "en"); err == nil {
		t.Fatal("Start was allowed with runtime not ready")
	}
}

func TestServiceInstallsMissingModels(t *testing.T) {
	sourceRoot, manifest := fakeAssets(t)
	parsed := mustManifest(t, manifest)
	packageRoot := t.TempDir()
	copyAssets(t, sourceRoot, packageRoot, parsed, runtimeToolAssets)
	dataDir := t.TempDir()
	s := NewService(packageRoot, dataDir, fakeRunner)
	if err := s.Initialize(context.Background(), manifest); err != nil {
		t.Fatal(err)
	}
	if status := s.Status(); status.Ready || status.RuntimeState != "required" {
		t.Fatalf("missing models did not request installation: %+v", status)
	}
	installedBytes := ModelInstalledSize(parsed)
	if status := s.Status(); status.RuntimeTotalBytes != installedBytes || status.RuntimeDownloadTotalBytes != 0 {
		t.Fatalf("model sizes were not initialized correctly: %+v", status)
	}
	s.install = func(_ context.Context, _ *http.Client, root string, runtime Manifest, progress RuntimeInstallProgress) error {
		progress("downloading", installedBytes/2, installedBytes)
		copyAssets(t, sourceRoot, root, runtime, runtimeModelAssets)
		progress("installing", installedBytes, installedBytes)
		return nil
	}
	if err := s.InstallModels(context.Background()); err != nil {
		t.Fatal(err)
	}
	status := s.Status()
	if !status.Ready || status.RuntimeState != "ready" || status.RuntimeProgress != 100 ||
		status.RuntimeTotalBytes != installedBytes || status.RuntimeDownloadTotalBytes != installedBytes {
		t.Fatalf("installed models not ready: %+v", status)
	}
	if s.engine.Root != packageRoot || s.engine.ModelRoot != dataDir {
		t.Fatalf("engine roots=%q/%q want %q/%q", s.engine.Root, s.engine.ModelRoot, packageRoot, dataDir)
	}
}

func TestServiceHandlesModelActivationCleanupWarning(t *testing.T) {
	setup := func(t *testing.T) (*Service, string, string, string, Manifest) {
		t.Helper()
		source, manifestData := fakeAssets(t)
		manifest := mustManifest(t, manifestData)
		packageRoot := t.TempDir()
		copyAssets(t, source, packageRoot, manifest, runtimeToolAssets)
		dataDir := t.TempDir()
		s := NewService(packageRoot, dataDir, fakeRunner)
		t.Cleanup(s.Close)
		if err := s.Initialize(context.Background(), manifestData); err != nil {
			t.Fatal(err)
		}
		return s, source, packageRoot, dataDir, manifest
	}
	cleanupWarning := func() error {
		return &modelActivationCleanupError{err: errors.New("remove replaced models: access denied")}
	}

	t.Run("ready with warning", func(t *testing.T) {
		s, source, _, dataDir, _ := setup(t)
		s.install = func(_ context.Context, _ *http.Client, root string, runtime Manifest, _ RuntimeInstallProgress) error {
			copyAssets(t, source, root, runtime, runtimeModelAssets)
			return cleanupWarning()
		}

		if err := s.InstallModels(context.Background()); err != nil {
			t.Fatal(err)
		}
		status := s.Status()
		if !status.Ready || status.RuntimeState != "ready" ||
			!strings.Contains(status.RuntimeError, "remove replaced models") {
			t.Fatalf("cleanup failure blocked the active runtime: %+v", status)
		}
		if s.engine.ModelRoot != dataDir {
			t.Fatalf("model root=%q want %q", s.engine.ModelRoot, dataDir)
		}
	})

	t.Run("unrelated activation failure", func(t *testing.T) {
		s, source, _, _, _ := setup(t)
		s.install = func(_ context.Context, _ *http.Client, root string, runtime Manifest, _ RuntimeInstallProgress) error {
			copyAssets(t, source, root, runtime, runtimeModelAssets)
			return errors.New("activate downloaded models: access denied")
		}

		err := s.InstallModels(context.Background())
		status := s.Status()
		if err == nil || status.Ready || status.RuntimeState != "failed" ||
			!strings.Contains(status.RuntimeError, "activate downloaded models") {
			t.Fatalf("activation failure became non-blocking: err=%v status=%+v", err, status)
		}
	})

	for _, test := range []struct {
		name    string
		message string
		damage  func(t *testing.T, packageRoot, dataDir string)
	}{
		{
			name:    "damaged models",
			message: "The downloaded models could not be verified",
			damage: func(t *testing.T, _, dataDir string) {
				t.Helper()
				path := filepath.Join(dataDir, "runtime", "models", "ggml-base.bin")
				if err := os.WriteFile(path, []byte("damaged"), 0600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name:    "damaged tools",
			message: "The bundled transcription tools could not be verified",
			damage: func(t *testing.T, packageRoot, _ string) {
				t.Helper()
				path := filepath.Join(packageRoot, "runtime", "whisper", "whisper-cli.exe")
				if err := os.WriteFile(path, []byte("damaged"), 0600); err != nil {
					t.Fatal(err)
				}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			s, source, packageRoot, dataDir, _ := setup(t)
			s.install = func(_ context.Context, _ *http.Client, root string, runtime Manifest, _ RuntimeInstallProgress) error {
				copyAssets(t, source, root, runtime, runtimeModelAssets)
				test.damage(t, packageRoot, dataDir)
				return cleanupWarning()
			}

			err := s.InstallModels(context.Background())
			status := s.Status()
			if err == nil || status.Ready || status.RuntimeState != "failed" ||
				status.RuntimeMessage != test.message || !strings.Contains(status.RuntimeError, "SHA-256") {
				t.Fatalf("verification failure became non-blocking: err=%v status=%+v", err, status)
			}
		})
	}
}

func TestServiceUsesBundledToolsWithManagedModels(t *testing.T) {
	source, manifestData := fakeAssets(t)
	manifest := mustManifest(t, manifestData)
	packageRoot := t.TempDir()
	dataDir := t.TempDir()
	copyAssets(t, source, packageRoot, manifest, runtimeToolAssets)
	copyAssets(t, source, dataDir, manifest, runtimeModelAssets)

	s := NewService(packageRoot, dataDir, fakeRunner)
	t.Cleanup(s.Close)
	if err := s.Initialize(context.Background(), manifestData); err != nil {
		t.Fatal(err)
	}
	status := s.Status()
	if !status.Ready || status.RuntimeState != "ready" {
		t.Fatalf("split runtime did not become ready: %+v", status)
	}
	if s.engine.Root != packageRoot || s.engine.ModelRoot != dataDir {
		t.Fatalf("engine roots=%q/%q want %q/%q", s.engine.Root, s.engine.ModelRoot, packageRoot, dataDir)
	}
}

func TestServiceUsesManagedV02RuntimeWhenBundledToolsAreMissing(t *testing.T) {
	source, manifestData := fakeAssets(t)
	manifest := mustManifest(t, manifestData)
	dataDir := t.TempDir()
	seedExistingRuntime(t, source, dataDir, manifest)

	s := NewService(t.TempDir(), dataDir, fakeRunner)
	t.Cleanup(s.Close)
	if err := s.Initialize(context.Background(), manifestData); err != nil {
		t.Fatal(err)
	}
	if status := s.Status(); !status.Ready || status.RuntimeState != "ready" {
		t.Fatalf("managed v0.2 runtime was not accepted: %+v", status)
	}
	if s.engine.Root != dataDir || s.engine.ModelRoot != dataDir {
		t.Fatalf("managed runtime roots=%q/%q want %q", s.engine.Root, s.engine.ModelRoot, dataDir)
	}
}

func TestServiceRejectsMissingBundledToolsWithoutLegacyRuntime(t *testing.T) {
	source, manifestData := fakeAssets(t)
	manifest := mustManifest(t, manifestData)
	dataDir := t.TempDir()
	copyAssets(t, source, dataDir, manifest, runtimeModelAssets)

	s := NewService(t.TempDir(), dataDir, fakeRunner)
	t.Cleanup(s.Close)
	err := s.Initialize(context.Background(), manifestData)
	if err == nil || !strings.Contains(err.Error(), "bundled transcription tools") {
		t.Fatalf("missing bundled tools error=%v", err)
	}
	if status := s.Status(); status.Ready || status.SetupError == "" {
		t.Fatalf("missing bundled tools were not fatal: %+v", status)
	}
}

func TestServiceRecoversInterruptedModelActivation(t *testing.T) {
	source, manifestData := fakeAssets(t)
	manifest := mustManifest(t, manifestData)
	packageRoot := t.TempDir()
	dataDir := t.TempDir()
	copyAssets(t, source, packageRoot, manifest, runtimeToolAssets)
	copyAssets(t, source, dataDir, manifest, runtimeModelAssets)
	backup := filepath.Join(dataDir, ".models-backup")
	if err := os.Rename(filepath.Join(dataDir, "runtime", "models"), backup); err != nil {
		t.Fatal(err)
	}

	s := NewService(packageRoot, dataDir, fakeRunner)
	t.Cleanup(s.Close)
	if err := s.Initialize(context.Background(), manifestData); err != nil {
		t.Fatal(err)
	}
	if status := s.Status(); !status.Ready || status.RuntimeError != "" {
		t.Fatalf("model backup was not recovered: %+v", status)
	}
	if _, err := os.Stat(backup); !os.IsNotExist(err) {
		t.Fatalf("model backup remained after recovery: %v", err)
	}
}

func TestServiceReportsInvalidModelBackup(t *testing.T) {
	source, manifestData := fakeAssets(t)
	manifest := mustManifest(t, manifestData)
	packageRoot := t.TempDir()
	dataDir := t.TempDir()
	copyAssets(t, source, packageRoot, manifest, runtimeToolAssets)
	copyAssets(t, source, dataDir, manifest, runtimeModelAssets)
	backup := filepath.Join(dataDir, ".models-backup")
	if err := os.Rename(filepath.Join(dataDir, "runtime", "models"), backup); err != nil {
		t.Fatal(err)
	}
	damaged := filepath.Join(backup, "ggml-base.bin")
	if err := os.WriteFile(damaged, []byte("damaged"), 0600); err != nil {
		t.Fatal(err)
	}

	s := NewService(packageRoot, dataDir, fakeRunner)
	t.Cleanup(s.Close)
	if err := s.Initialize(context.Background(), manifestData); err != nil {
		t.Fatal(err)
	}
	status := s.Status()
	if status.Ready || status.RuntimeState != "required" ||
		!strings.Contains(status.RuntimeError, "recover interrupted model update") ||
		!strings.Contains(status.RuntimeError, "backup is invalid") {
		t.Fatalf("invalid model backup was not reported as retryable: %+v", status)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "runtime", "models")); !os.IsNotExist(err) {
		t.Fatalf("invalid model backup remained active: %v", err)
	}
	if _, err := os.Stat(damaged); err != nil {
		t.Fatalf("invalid model backup was not restored: %v", err)
	}
}

func TestServiceRemovesModelBackupAfterManagedModelsVerify(t *testing.T) {
	source, manifestData := fakeAssets(t)
	manifest := mustManifest(t, manifestData)
	packageRoot := t.TempDir()
	dataDir := t.TempDir()
	copyAssets(t, source, packageRoot, manifest, runtimeToolAssets)
	copyAssets(t, source, dataDir, manifest, runtimeModelAssets)
	backup := filepath.Join(dataDir, ".models-backup")
	if err := os.Rename(filepath.Join(dataDir, "runtime", "models"), backup); err != nil {
		t.Fatal(err)
	}
	copyAssets(t, source, dataDir, manifest, runtimeModelAssets)

	s := NewService(packageRoot, dataDir, fakeRunner)
	t.Cleanup(s.Close)
	if err := s.Initialize(context.Background(), manifestData); err != nil {
		t.Fatal(err)
	}
	if status := s.Status(); !status.Ready || status.RuntimeError != "" {
		t.Fatalf("valid managed models were not selected: %+v", status)
	}
	if _, err := os.Stat(backup); !os.IsNotExist(err) {
		t.Fatalf("stale model backup remained: %v", err)
	}
}

func TestServiceCleansInterruptedRuntimeInstallArtifacts(t *testing.T) {
	root, manifest := fakeAssets(t)
	dataDir := t.TempDir()
	download := filepath.Join(dataDir, ".runtime-download-1234567890.zip")
	stage := filepath.Join(dataDir, ".runtime-install-1234567890")
	modelStage := filepath.Join(dataDir, ".model-install-1234567890")
	if err := os.WriteFile(download, []byte("partial download"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(stage, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stage, "partial-runtime"), []byte("partial install"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(modelStage, 0700); err != nil {
		t.Fatal(err)
	}
	unrelated := []string{
		".runtime-download-user.zip",
		".runtime-download-12345678901.zip",
		".runtime-download-1234.tmp",
		".runtime-install-user",
		".model-install-user",
	}
	for _, name := range unrelated {
		if err := os.WriteFile(filepath.Join(dataDir, name), []byte("keep"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	wrongTypeDownload := filepath.Join(dataDir, ".runtime-download-1234.zip")
	if err := os.Mkdir(wrongTypeDownload, 0700); err != nil {
		t.Fatal(err)
	}
	wrongTypeStage := filepath.Join(dataDir, ".runtime-install-1234")
	if err := os.WriteFile(wrongTypeStage, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}

	s := NewService(root, dataDir, fakeRunner)
	t.Cleanup(s.Close)
	if err := s.Initialize(context.Background(), manifest); err != nil {
		t.Fatal(err)
	}
	if status := s.Status(); !status.Ready || status.RuntimeError != "" {
		t.Fatalf("runtime cleanup changed service readiness: %+v", status)
	}
	for _, path := range []string{download, stage, modelStage} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("orphaned runtime artifact remained at %s: %v", path, err)
		}
	}
	for _, name := range unrelated {
		if _, err := os.Stat(filepath.Join(dataDir, name)); err != nil {
			t.Fatalf("unrelated entry %s was removed: %v", name, err)
		}
	}
	for _, path := range []string{wrongTypeDownload, wrongTypeStage} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("wrong-type entry %s was removed: %v", path, err)
		}
	}
}

func TestServiceRemovesBackupAfterManagedRuntimeVerifies(t *testing.T) {
	source, manifestData := fakeAssets(t)
	manifest := mustManifest(t, manifestData)
	dataDir := t.TempDir()
	seedExistingRuntime(t, source, dataDir, manifest)
	backup := filepath.Join(dataDir, ".runtime-backup")
	if err := os.Rename(filepath.Join(dataDir, "runtime"), backup); err != nil {
		t.Fatal(err)
	}
	seedExistingRuntime(t, source, dataDir, manifest)

	s := NewService(t.TempDir(), dataDir, fakeRunner)
	t.Cleanup(s.Close)
	if err := s.Initialize(context.Background(), manifestData); err != nil {
		t.Fatal(err)
	}
	status := s.Status()
	if !status.Ready || status.RuntimeState != "ready" || status.RuntimeError != "" {
		t.Fatalf("valid managed runtime was not selected: %+v", status)
	}
	if s.engine.Root != dataDir {
		t.Fatalf("managed runtime root=%q want %q", s.engine.Root, dataDir)
	}
	if _, err := os.Stat(backup); !os.IsNotExist(err) {
		t.Fatalf("runtime backup remained after managed runtime verification: %v", err)
	}
}

func TestServiceRecoversInterruptedRuntimeActivation(t *testing.T) {
	source, manifestData := fakeAssets(t)
	manifest := mustManifest(t, manifestData)
	dataDir := t.TempDir()
	seedExistingRuntime(t, source, dataDir, manifest)
	if err := os.Rename(filepath.Join(dataDir, "runtime"), filepath.Join(dataDir, ".runtime-backup")); err != nil {
		t.Fatal(err)
	}
	s := NewService(t.TempDir(), dataDir, fakeRunner)
	t.Cleanup(s.Close)

	if err := s.Initialize(context.Background(), manifestData); err != nil {
		t.Fatal(err)
	}
	status := s.Status()
	if !status.Ready || status.RuntimeState != "ready" || status.RuntimeError != "" {
		t.Fatalf("runtime backup was not recovered: %+v", status)
	}
	if s.engine.Root != dataDir {
		t.Fatalf("recovered runtime root=%q want %q", s.engine.Root, dataDir)
	}
	if err := VerifyManifest(context.Background(), dataDir, manifest); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, ".runtime-backup")); !os.IsNotExist(err) {
		t.Fatalf("runtime backup remained after recovery: %v", err)
	}
}

func TestServiceReportsInvalidRuntimeBackup(t *testing.T) {
	source, manifestData := fakeAssets(t)
	manifest := mustManifest(t, manifestData)
	packageRoot := t.TempDir()
	copyAssets(t, source, packageRoot, manifest, runtimeToolAssets)
	dataDir := t.TempDir()
	seedExistingRuntime(t, source, dataDir, manifest)
	if err := os.Rename(filepath.Join(dataDir, "runtime"), filepath.Join(dataDir, ".runtime-backup")); err != nil {
		t.Fatal(err)
	}
	damaged := filepath.Join(dataDir, ".runtime-backup", "models", "ggml-base.bin")
	if err := os.WriteFile(damaged, []byte("damaged"), 0600); err != nil {
		t.Fatal(err)
	}
	s := NewService(packageRoot, dataDir, fakeRunner)
	t.Cleanup(s.Close)

	if err := s.Initialize(context.Background(), manifestData); err != nil {
		t.Fatal(err)
	}
	status := s.Status()
	if status.Ready || status.RuntimeState != "required" || status.SetupError != "" ||
		!strings.Contains(status.RuntimeError, "recover interrupted runtime update") ||
		!strings.Contains(status.RuntimeError, "backup is invalid") {
		t.Fatalf("invalid runtime backup was not reported as retryable: %+v", status)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "runtime", "models")); !os.IsNotExist(err) {
		t.Fatalf("invalid backup models remained active: %v", err)
	}
	if _, err := os.Stat(damaged); err != nil {
		t.Fatalf("invalid backup was not returned to its original location: %v", err)
	}
}

func TestServiceDoesNotReplacePresentRuntimeWithBackup(t *testing.T) {
	source, manifestData := fakeAssets(t)
	manifest := mustManifest(t, manifestData)
	packageRoot := t.TempDir()
	copyAssets(t, source, packageRoot, manifest, runtimeToolAssets)
	dataDir := t.TempDir()
	seedExistingRuntime(t, source, dataDir, manifest)
	if err := os.Rename(filepath.Join(dataDir, "runtime"), filepath.Join(dataDir, ".runtime-backup")); err != nil {
		t.Fatal(err)
	}
	backupMarker := filepath.Join(dataDir, ".runtime-backup", "backup-only.txt")
	if err := os.WriteFile(backupMarker, []byte("older backup"), 0600); err != nil {
		t.Fatal(err)
	}
	seedExistingRuntime(t, source, dataDir, manifest)
	damaged := filepath.Join(dataDir, "runtime", "models", "ggml-base.bin")
	if err := os.WriteFile(damaged, []byte("damaged target"), 0600); err != nil {
		t.Fatal(err)
	}
	s := NewService(packageRoot, dataDir, fakeRunner)
	t.Cleanup(s.Close)

	if err := s.Initialize(context.Background(), manifestData); err != nil {
		t.Fatal(err)
	}
	status := s.Status()
	if status.Ready || status.RuntimeState != "required" || status.RuntimeError != "" {
		t.Fatalf("present damaged runtime should request a retry without backup recovery: %+v", status)
	}
	data, err := os.ReadFile(damaged)
	if err != nil || string(data) != "damaged target" {
		t.Fatalf("present runtime was replaced: %q err=%v", data, err)
	}
	if data, err := os.ReadFile(backupMarker); err != nil || string(data) != "older backup" {
		t.Fatalf("older backup changed: %q err=%v", data, err)
	}
}

func mustManifest(t *testing.T, data []byte) Manifest {
	t.Helper()
	manifest, err := ParseManifest(data)
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}

// A damaged saved transcript must not block the healthy ones from loading, and
// must be preserved in place for the user to recover by hand.
func TestDamagedHistoryFilePreservedAndReported(t *testing.T) {
	root, manifest := fakeAssets(t)
	dataDir := t.TempDir()
	storeDir := filepath.Join(dataDir, "transcripts")
	if err := os.MkdirAll(storeDir, 0700); err != nil {
		t.Fatal(err)
	}
	saved := exampleTranscript()
	if err := (Store{Dir: storeDir}).Save(saved); err != nil {
		t.Fatal(err)
	}
	damaged := strings.Repeat("d", 32) + ".json"
	if err := os.WriteFile(filepath.Join(storeDir, damaged), []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	s := NewService(root, dataDir, fakeRunner)
	if err := s.Initialize(context.Background(), manifest); err != nil {
		t.Fatal(err)
	}
	status := s.Status()
	if !status.Ready {
		t.Fatal("service did not become ready alongside a damaged history file")
	}
	if len(status.History) != 1 || status.History[0].ID != saved.ID {
		t.Fatalf("healthy history missing: %+v", status.History)
	}
	if status.HistoryWarning == "" || !strings.Contains(status.HistoryWarning, damaged) {
		t.Fatalf("damaged file was not reported to the user: %q", status.HistoryWarning)
	}
	if _, err := os.Stat(filepath.Join(storeDir, damaged)); err != nil {
		t.Fatal("damaged history file must be preserved on disk")
	}
}
