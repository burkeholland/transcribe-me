package transcribe

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"
)

type Runner func(context.Context, string, []string, func(string)) ([]byte, error)

type processOutput struct {
	mu      sync.Mutex
	tail    []byte
	pending []byte
	onLine  func(string)
}

// Retain diagnostics, not hours of transcript output or unbounded decoder logs.
func (w *processOutput) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.tail = append(w.tail, p...)
	if len(w.tail) > 64*1024 {
		w.tail = w.tail[len(w.tail)-64*1024:]
	}
	if w.onLine != nil {
		w.pending = append(w.pending, p...)
		for {
			i := bytes.IndexAny(w.pending, "\r\n")
			if i < 0 {
				break
			}
			w.onLine(string(w.pending[:i]))
			w.pending = w.pending[i+1:]
		}
		if len(w.pending) > 64*1024 {
			w.pending = nil
		}
	}
	return len(p), nil
}

func RunProcess(ctx context.Context, executable string, args []string, onLine func(string)) ([]byte, error) {
	cmd := exec.CommandContext(ctx, executable, args...)
	configureProcess(cmd)
	cmd.WaitDelay = 3 * time.Second
	stdout := &processOutput{onLine: onLine}
	stderr := &processOutput{onLine: onLine}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Stdin = nil
	err := cmd.Run()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(stderr.tail)))
	}
	return stdout.tail, nil
}

var _ io.Writer = (*processOutput)(nil)
