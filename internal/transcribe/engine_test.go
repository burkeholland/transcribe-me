package transcribe

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const probeJSON = `{"streams":[{"codec_type":"video"},{"codec_type":"audio"}],"format":{"duration":"12.5"}}`
const transcriptJSON = `{"result":{"language":"en"},"transcription":[{"offsets":{"from":0,"to":1250},"text":" Hello there."},{"offsets":{"from":1250,"to":2500},"text":" This stays local."}]}`

func testMedia(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "video with spaces & punctuation.mp4")
	if err := os.WriteFile(path, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func fakeRunner(ctx context.Context, exe string, args []string, progress func(string)) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	switch filepath.Base(exe) {
	case "ffprobe.exe":
		return []byte(probeJSON), nil
	case "ffmpeg.exe":
		if progress != nil {
			progress("out_time_us=12500000")
		}
		return nil, nil
	case "whisper-cli.exe":
		if progress != nil {
			progress("whisper_print_progress_callback: progress = 100%")
		}
		for i, a := range args {
			if a == "-of" {
				return nil, os.WriteFile(args[i+1]+".json", []byte(transcriptJSON), 0600)
			}
		}
	}
	return nil, errors.New("unexpected test process")
}

func TestInspect(t *testing.T) {
	path := testMedia(t)
	e := Engine{Root: t.TempDir(), Run: fakeRunner}
	info, err := e.Inspect(context.Background(), path)
	if err != nil || info.DurationMs != 12500 || info.Name != filepath.Base(path) {
		t.Fatalf("info=%+v err=%v", info, err)
	}
	for _, path := range []string{"relative.mp4", filepath.Join(t.TempDir(), "missing.mp4"), filepath.Join(t.TempDir(), "video.exe"), t.TempDir()} {
		if _, err := e.Inspect(context.Background(), path); err == nil {
			t.Fatalf("accepted invalid path %q", path)
		}
	}
}

func TestInspectBadMetadata(t *testing.T) {
	path := testMedia(t)
	for _, tc := range []struct{ name, json, expected string }{
		{"no audio", `{"streams":[],"format":{"duration":"1"}}`, "no audio"},
		{"invalid", `{`, "metadata"},
		{"duration", `{"streams":[{"codec_type":"audio"}],"format":{"duration":"NaN"}}`, "duration"},
		{"infinite", `{"streams":[{"codec_type":"audio"}],"format":{"duration":"Inf"}}`, "duration"},
		{"zero", `{"streams":[{"codec_type":"audio"}],"format":{"duration":"0"}}`, "duration"},
		{"too long", `{"streams":[{"codec_type":"audio"}],"format":{"duration":"21601"}}`, "6 hours"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := Engine{Run: func(context.Context, string, []string, func(string)) ([]byte, error) {
				return []byte(tc.json), nil
			}}
			if _, err := e.Inspect(context.Background(), path); err == nil || !strings.Contains(err.Error(), tc.expected) {
				t.Fatalf("expected %s, got %v", tc.expected, err)
			}
		})
	}
}

func TestTranscribe(t *testing.T) {
	e := Engine{Root: t.TempDir(), Run: fakeRunner}
	var progress float64
	result, err := e.Transcribe(context.Background(), FileInfo{Name: "test.mp4", DurationMs: 12500}, "auto", t.TempDir(), func(_ string, p float64, _ string) { progress = p })
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "Hello there.\nThis stays local." || result.WordCount != 5 || result.Language != "en" || len(result.Segments) != 2 || progress != 95 {
		t.Fatalf("unexpected transcript=%+v progress=%v", result, progress)
	}
}

func TestTranscribeErrors(t *testing.T) {
	for _, tc := range []struct{ name, json, expected string }{
		{"broken JSON", `{`, "read Whisper output"},
		{"missing result", `{}`, "incomplete"},
		{"negative timestamp", `{"result":{"language":"en"},"transcription":[{"offsets":{"from":-1,"to":500},"text":"x"}]}`, "timestamps"},
		{"inverted timestamp", `{"result":{"language":"en"},"transcription":[{"offsets":{"from":500,"to":0},"text":"x"}]}`, "timestamps"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := Engine{Run: func(ctx context.Context, exe string, args []string, cb func(string)) ([]byte, error) {
				if filepath.Base(exe) == "whisper-cli.exe" {
					for i, a := range args {
						if a == "-of" {
							return nil, os.WriteFile(args[i+1]+".json", []byte(tc.json), 0600)
						}
					}
				}
				return fakeRunner(ctx, exe, args, cb)
			}}
			_, err := e.Transcribe(context.Background(), FileInfo{DurationMs: 12500}, "en", t.TempDir(), func(string, float64, string) {})
			if err == nil || !strings.Contains(err.Error(), tc.expected) {
				t.Fatalf("expected %s, got %v", tc.expected, err)
			}
		})
	}
}

func TestArgumentsStayLocal(t *testing.T) {
	e := Engine{Run: func(ctx context.Context, exe string, args []string, cb func(string)) ([]byte, error) {
		joined := strings.Join(args, "|")
		switch filepath.Base(exe) {
		case "ffmpeg.exe", "ffprobe.exe":
			if !strings.Contains(joined, "-protocol_whitelist|file,pipe") || !strings.Contains(joined, "-format_whitelist|") {
				t.Error("media process can open network URLs or playlists")
			}
		case "whisper-cli.exe":
			if !strings.Contains(joined, "-ng|") || !strings.Contains(joined, "--vad|") {
				t.Error("CPU mode or speech detection is missing")
			}
		}
		return fakeRunner(ctx, exe, args, cb)
	}}
	file, err := e.Inspect(context.Background(), testMedia(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Transcribe(context.Background(), file, "en", t.TempDir(), func(string, float64, string) {}); err != nil {
		t.Fatal(err)
	}
}
