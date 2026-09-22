//go:build e2e

package main

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"transcribeme/internal/transcribe"
)

func TestRealVideoEndToEnd(t *testing.T) {
	video := os.Getenv("E2E_VIDEO")
	if video == "" {
		t.Skip("set E2E_VIDEO")
	}
	root := os.Getenv("E2E_ROOT")
	data := t.TempDir()
	svc := transcribe.NewService(root, data, transcribe.RunProcess)
	ctx := context.Background()
	if err := svc.Initialize(ctx, runtimeManifest); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	defer svc.Close()

	info, err := svc.Inspect(ctx, video)
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	t.Logf("inspect: name=%s duration=%dms height=%d size=%d", info.Name, info.DurationMs, info.Height, info.Size)
	if info.Height <= 0 {
		t.Errorf("expected a video height, got %d", info.Height)
	}

	if err := svc.Start(ctx, video, "auto"); err != nil {
		t.Fatalf("start: %v", err)
	}
	deadline := time.Now().Add(10 * time.Minute)
	var id string
	for time.Now().Before(deadline) {
		snap := svc.Status()
		if snap.Job == nil {
			time.Sleep(500 * time.Millisecond)
			continue
		}
		switch snap.Job.State {
		case "completed":
			id = snap.Job.TranscriptID
		case "failed", "cancelled":
			t.Fatalf("job %s: %s", snap.Job.State, snap.Job.Error)
		}
		if id != "" {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if id == "" {
		t.Fatal("job did not finish in time")
	}

	tr, err := svc.Get(id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	t.Logf("transcript: words=%d segments=%d language=%s", tr.WordCount, len(tr.Segments), tr.Language)
	t.Logf("text: %.400s", tr.Text)
	if len(tr.Segments) == 0 || strings.TrimSpace(tr.Text) == "" {
		t.Fatal("expected a non-empty transcript")
	}

	snap := svc.Status()
	if len(snap.History) == 0 {
		t.Fatal("expected the transcript in history")
	}
	h := snap.History[0]
	t.Logf("history: file=%s height=%d size=%d source=%s", h.FileName, h.Height, h.Size, h.SourcePath)
	if h.Height != info.Height || h.Size != info.Size || h.SourcePath != video {
		t.Errorf("summary metadata not persisted: %+v", h)
	}
}
