package transcribe

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
	if err := s.Initialize(context.Background(), manifest); err == nil || s.Status().Ready || s.Status().SetupError == "" {
		t.Fatal("damaged runtime reported ready")
	}
	for _, data := range []string{`{`, `{"files":[]}`, `{"files":[{"path":"../outside"}]}`} {
		if err := VerifyAssets(context.Background(), root, []byte(data)); err == nil {
			t.Fatalf("accepted invalid manifest %s", data)
		}
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
	if err := s.Initialize(context.Background(), manifest); err == nil {
		t.Fatal("damaged runtime was accepted")
	}
	status := s.Status()
	if status.Ready {
		t.Fatal("service reported ready with a damaged runtime")
	}
	if status.SetupError == "" {
		t.Fatal("setup error was not surfaced")
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
