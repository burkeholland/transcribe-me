package transcribe

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
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
		m.Files = append(m.Files, Asset{Path: name, Size: int64(len(data)), SHA256: hex.EncodeToString(sum[:])})
	}
	manifest, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return root, manifest
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
	root, manifest := fakeAssets(t)
	if err := VerifyAssets(context.Background(), root, manifest); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "runtime", "models", "ggml-base.bin"), []byte("damaged"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := VerifyAssets(context.Background(), root, manifest); err == nil || !strings.Contains(err.Error(), "SHA-256") {
		t.Fatalf("expected hash rejection: %v", err)
	}
	s := NewService(root, t.TempDir(), fakeRunner)
	if err := s.Initialize(context.Background(), manifest); err != nil {
		t.Fatal(err)
	}
	status := s.Status()
	if status.Ready || status.RuntimeState != "required" || status.SetupError != "" {
		t.Fatalf("damaged runtime did not request repair: %+v", status)
	}
	for _, data := range []string{`{`, `{"files":[]}`, `{"files":[{"path":"../outside"}]}`} {
		if err := VerifyAssets(context.Background(), root, []byte(data)); err == nil {
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

func TestServiceInstallsMissingRuntime(t *testing.T) {
	sourceRoot, manifest := fakeAssets(t)
	dataDir := t.TempDir()
	s := NewService(t.TempDir(), dataDir, fakeRunner)
	if err := s.Initialize(context.Background(), manifest); err != nil {
		t.Fatal(err)
	}
	if status := s.Status(); status.Ready || status.RuntimeState != "required" {
		t.Fatalf("missing runtime did not request installation: %+v", status)
	}
	installedBytes := RuntimeInstalledSize(mustManifest(t, manifest))
	if status := s.Status(); status.RuntimeTotalBytes != installedBytes || status.RuntimeDownloadTotalBytes != 0 {
		t.Fatalf("runtime sizes were not initialized correctly: %+v", status)
	}
	s.install = func(_ context.Context, _ *http.Client, root, _ string, _ []byte, progress RuntimeInstallProgress) error {
		progress("downloading", 4, 8)
		for _, asset := range mustManifest(t, manifest).Files {
			source := filepath.Join(sourceRoot, filepath.FromSlash(asset.Path))
			target := filepath.Join(root, filepath.FromSlash(asset.Path))
			if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
				return err
			}
			input, err := os.Open(source)
			if err != nil {
				return err
			}
			output, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
			if err != nil {
				input.Close()
				return err
			}
			_, copyErr := io.Copy(output, input)
			closeErr := errors.Join(input.Close(), output.Close())
			if err := errors.Join(copyErr, closeErr); err != nil {
				return err
			}
		}
		progress("installing", 8, 8)
		return nil
	}
	if err := s.InstallRuntime(context.Background(), "https://example.invalid/runtime.zip"); err != nil {
		t.Fatal(err)
	}
	status := s.Status()
	if !status.Ready || status.RuntimeState != "ready" || status.RuntimeProgress != 100 ||
		status.RuntimeTotalBytes != installedBytes || status.RuntimeDownloadTotalBytes != 8 {
		t.Fatalf("installed runtime not ready: %+v", status)
	}
}

func TestServiceCleansInterruptedRuntimeInstallArtifacts(t *testing.T) {
	root, manifest := fakeAssets(t)
	dataDir := t.TempDir()
	download := filepath.Join(dataDir, ".runtime-download-1234567890.zip")
	stage := filepath.Join(dataDir, ".runtime-install-1234567890")
	if err := os.WriteFile(download, []byte("partial download"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(stage, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stage, "partial-runtime"), []byte("partial install"), 0600); err != nil {
		t.Fatal(err)
	}
	unrelated := []string{
		".runtime-download-user.zip",
		".runtime-download-12345678901.zip",
		".runtime-download-1234.tmp",
		".runtime-install-user",
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
	for _, path := range []string{download, stage} {
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
	if err := VerifyAssets(context.Background(), dataDir, manifestData); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, ".runtime-backup")); !os.IsNotExist(err) {
		t.Fatalf("runtime backup remained after recovery: %v", err)
	}
}

func TestServiceReportsInvalidRuntimeBackup(t *testing.T) {
	source, manifestData := fakeAssets(t)
	manifest := mustManifest(t, manifestData)
	dataDir := t.TempDir()
	seedExistingRuntime(t, source, dataDir, manifest)
	if err := os.Rename(filepath.Join(dataDir, "runtime"), filepath.Join(dataDir, ".runtime-backup")); err != nil {
		t.Fatal(err)
	}
	damaged := filepath.Join(dataDir, ".runtime-backup", "models", "ggml-base.bin")
	if err := os.WriteFile(damaged, []byte("damaged"), 0600); err != nil {
		t.Fatal(err)
	}
	s := NewService(t.TempDir(), dataDir, fakeRunner)
	t.Cleanup(s.Close)

	if err := s.Initialize(context.Background(), manifestData); err != nil {
		t.Fatal(err)
	}
	status := s.Status()
	if status.Ready || status.RuntimeState != "required" || status.SetupError != "" ||
		!strings.Contains(status.RuntimeError, "recover interrupted runtime update") ||
		!strings.Contains(status.RuntimeError, "runtime backup is invalid") {
		t.Fatalf("invalid runtime backup was not reported as retryable: %+v", status)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "runtime")); !os.IsNotExist(err) {
		t.Fatalf("invalid backup remained active: %v", err)
	}
	if _, err := os.Stat(damaged); err != nil {
		t.Fatalf("invalid backup was not returned to its original location: %v", err)
	}
}

func TestServiceDoesNotReplacePresentRuntimeWithBackup(t *testing.T) {
	source, manifestData := fakeAssets(t)
	manifest := mustManifest(t, manifestData)
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
	s := NewService(t.TempDir(), dataDir, fakeRunner)
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
