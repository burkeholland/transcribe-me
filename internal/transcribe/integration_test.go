package transcribe

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func realRuntime(t *testing.T) (string, string, []byte) {
	t.Helper()
	root := os.Getenv("TRANSCRIBEME_TEST_RUNTIME_DIR")
	if root == "" {
		var err error
		root, err = filepath.Abs(filepath.Join("..", ".."))
		if err != nil {
			t.Fatal(err)
		}
	} else {
		if !filepath.IsAbs(root) {
			t.Fatal("TRANSCRIBEME_TEST_RUNTIME_DIR must be an absolute path containing the runtime directory")
		}
	}
	manifest, err := os.ReadFile(filepath.Join("..", "..", "assets", "runtime-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	dataDir := os.Getenv("TRANSCRIBEME_TEST_DATA_DIR")
	if dataDir != "" && !filepath.IsAbs(dataDir) {
		t.Fatal("TRANSCRIBEME_TEST_DATA_DIR must be an absolute path containing the downloaded models")
	}
	return root, dataDir, manifest
}

func TestRealSilenceAndCancellation(t *testing.T) {
	video := os.Getenv("TRANSCRIBEME_TEST_VIDEO")
	if video == "" {
		t.Skip("set TRANSCRIBEME_TEST_VIDEO to exercise native runtime edge cases")
	}
	root, dataDir, manifest := realRuntime(t)
	if dataDir == "" {
		dataDir = t.TempDir()
	}
	s := NewService(root, dataDir, RunProcess)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	defer s.Close()
	if err := s.Initialize(ctx, manifest); err != nil {
		t.Fatal(err)
	}
	historyBefore := len(s.Status().History)
	// A standard PCM WAV fixture avoids relying on any installed encoder.
	wav := make([]byte, 44+16000*2*2)
	copy(wav, "RIFF")
	binary.LittleEndian.PutUint32(wav[4:8], uint32(len(wav)-8))
	copy(wav[8:16], "WAVEfmt ")
	binary.LittleEndian.PutUint32(wav[16:20], 16)
	binary.LittleEndian.PutUint16(wav[20:22], 1)
	binary.LittleEndian.PutUint16(wav[22:24], 1)
	binary.LittleEndian.PutUint32(wav[24:28], 16000)
	binary.LittleEndian.PutUint32(wav[28:32], 32000)
	binary.LittleEndian.PutUint16(wav[32:34], 2)
	binary.LittleEndian.PutUint16(wav[34:36], 16)
	copy(wav[36:40], "data")
	binary.LittleEndian.PutUint32(wav[40:44], uint32(len(wav)-44))
	path := filepath.Join(t.TempDir(), "silence with spaces.wav")
	if err := os.WriteFile(path, wav, 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.Start(ctx, path, "auto"); err != nil {
		t.Fatal(err)
	}
	status := waitService(t, s)
	if status.Job.State != "completed" {
		t.Fatalf("silence did not complete: %+v", status.Job)
	}
	result, err := s.Get(status.Job.TranscriptID)
	if err != nil || result.Text != "" || len(result.Segments) != 0 {
		t.Fatalf("silence hallucinated speech: %+v %v", result, err)
	}
	if err := s.Start(ctx, video, "en"); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(20 * time.Second)
	for s.Status().Job.State != "transcribing" {
		select {
		case <-deadline:
			t.Fatalf("transcriber did not start: %+v", s.Status().Job)
		case <-time.After(20 * time.Millisecond):
		}
	}
	if err := s.Cancel(); err != nil {
		t.Fatal(err)
	}
	status = waitService(t, s)
	if status.Job.State != "cancelled" || len(status.History) != historyBefore+1 {
		t.Fatalf("native cancellation failed: %+v", status)
	}
	entries, err := os.ReadDir(filepath.Join(s.dataDir, "work"))
	if err != nil || len(entries) > 0 {
		t.Fatal("native cancellation left temporary audio")
	}
	badPath := filepath.Join(t.TempDir(), "corrupt.mp4")
	if err := os.WriteFile(badPath, []byte("not a media file"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Inspect(ctx, badPath); err == nil || !strings.Contains(err.Error(), "cannot read") {
		t.Fatalf("corrupt media accepted: %v", err)
	}
}

func TestRealVideo(t *testing.T) {
	video := os.Getenv("TRANSCRIBEME_TEST_VIDEO")
	if video == "" {
		t.Skip("set TRANSCRIBEME_TEST_VIDEO to run real offline inference")
	}
	root, dataDir, manifest := realRuntime(t)
	hashFile := func() string {
		f, err := os.Open(video)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		h := sha256.New()
		if _, err := io.Copy(h, f); err != nil {
			t.Fatal(err)
		}
		return string(h.Sum(nil))
	}
	before := hashFile()
	if dataDir == "" {
		dataDir = filepath.Join(t.TempDir(), "Local data \u65e5\u672c")
	}
	s := NewService(root, dataDir, RunProcess)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()
	defer s.Close()
	if err := s.Initialize(ctx, manifest); err != nil {
		t.Fatal(err)
	}
	if err := s.Start(ctx, video, "en"); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	done := s.done
	s.mu.Unlock()
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("real transcription timed out")
	}
	status := s.Status()
	if status.Job.State != "completed" {
		t.Fatalf("real inference failed: %+v", status.Job)
	}
	result, err := s.Get(status.Job.TranscriptID)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Segments) < 2 || result.WordCount < 10 || result.DurationMs < 1000 {
		t.Fatalf("no meaningful speech recovered: %+v", result)
	}
	if hashFile() != before {
		t.Fatal("source video changed")
	}
	for _, format := range []string{"txt", "srt", "vtt"} {
		data, err := Format(result, format)
		if err != nil || len(data) < 10 {
			t.Fatalf("%s export failed: %v", format, err)
		}
	}
	entries, err := os.ReadDir(filepath.Join(s.dataDir, "work"))
	if err != nil || len(entries) != 0 {
		t.Fatal("temporary audio was not removed")
	}
	if out := os.Getenv("TRANSCRIBEME_TEST_OUTPUT"); out != "" {
		data, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := WriteAtomic(out, data); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("Real video: %s; duration=%dms; words=%d; segments=%d; language=%s", result.FileName, result.DurationMs, result.WordCount, len(result.Segments), result.Language)
}

func TestRealSupportedFormats(t *testing.T) {
	if os.Getenv("TRANSCRIBEME_TEST_VIDEO") == "" {
		t.Skip("set TRANSCRIBEME_TEST_VIDEO to exercise real supported media formats")
	}
	root, dataDir, manifest := realRuntime(t)
	if dataDir == "" {
		dataDir = filepath.Join(t.TempDir(), "Codec profile \u65e5\u672c")
	}
	s := NewService(root, dataDir, RunProcess)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	defer s.Close()
	if err := s.Initialize(ctx, manifest); err != nil {
		t.Fatal(err)
	}
	fixtures, err := filepath.Abs(filepath.Join("..", "..", "scripts", "fixtures"))
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(fixtures)
	if err != nil {
		t.Fatal(err)
	}
	tested := 0
	for _, entry := range entries {
		if entry.IsDir() || !supported(filepath.Ext(entry.Name()), mediaExtensions) {
			continue
		}
		tested++
		t.Run(entry.Name(), func(t *testing.T) {
			if err := s.Start(ctx, filepath.Join(fixtures, entry.Name()), "en"); err != nil {
				t.Fatal(err)
			}
			s.mu.Lock()
			done := s.done
			s.mu.Unlock()
			select {
			case <-done:
			case <-ctx.Done():
				t.Fatal("native format validation timed out")
			}
			status := s.Status()
			if status.Job.State != "completed" {
				t.Fatalf("format failed through production pipeline: %+v", status.Job)
			}
			if _, err := s.Get(status.Job.TranscriptID); err != nil {
				t.Fatal(err)
			}
		})
	}
	if tested < 17 {
		t.Fatalf("expected the full codec fixture matrix, found %d files", tested)
	}
}
