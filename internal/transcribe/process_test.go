package transcribe

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestProcessHelper(t *testing.T) {
	if len(os.Args) < 2 || os.Args[len(os.Args)-2] != "--" {
		return
	}
	switch os.Args[len(os.Args)-1] {
	case "success":
		fmt.Println("progress=100")
		os.Exit(0)
	case "fail":
		fmt.Fprintln(os.Stderr, "test diagnostic")
		os.Exit(7)
	case "block":
		time.Sleep(30 * time.Second)
		os.Exit(0)
	}
}

func TestRunProcess(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	var line string
	data, err := RunProcess(context.Background(), exe, []string{"-test.run=TestProcessHelper", "--", "success"}, func(s string) { line = s })
	if err != nil || line != "progress=100" || !strings.Contains(string(data), "progress=100") {
		t.Fatalf("stdout=%q line=%q err=%v", data, line, err)
	}
	_, err = RunProcess(context.Background(), exe, []string{"-test.run=TestProcessHelper", "--", "fail"}, nil)
	if err == nil || !strings.Contains(err.Error(), "test diagnostic") {
		t.Fatalf("diagnostic was lost: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err = RunProcess(ctx, exe, []string{"-test.run=TestProcessHelper", "--", "block"}, nil)
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 4*time.Second {
		t.Fatalf("process cancellation failed: %v", err)
	}
}

func TestProcessOutputBounded(t *testing.T) {
	var lines []string
	w := &processOutput{onLine: func(s string) { lines = append(lines, s) }}
	_, _ = w.Write([]byte("one\r"))
	_, _ = w.Write([]byte("\ntw"))
	_, _ = w.Write([]byte("o\n"))
	_, _ = w.Write([]byte(strings.Repeat("x", 100000)))
	if len(w.tail) > 65536 || len(w.pending) > 65536 || strings.Join(lines, "|") != "one||two" {
		t.Fatalf("output buffering failed: %+v", lines)
	}
}
