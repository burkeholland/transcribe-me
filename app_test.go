package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestAppPaths(t *testing.T) {
	base := t.TempDir()
	t.Setenv("LOCALAPPDATA", base)
	t.Setenv("TRANSCRIBEME_RUNTIME_DIR", "")
	t.Setenv("TRANSCRIBEME_DATA_DIR", "")
	root, data, err := appPaths()
	if err != nil || !filepath.IsAbs(root) || data != filepath.Join(base, "TranscribeMe") {
		t.Fatalf("root=%s data=%s err=%v", root, data, err)
	}
	t.Setenv("TRANSCRIBEME_RUNTIME_DIR", filepath.Join(base, "runtime-root"))
	t.Setenv("TRANSCRIBEME_DATA_DIR", filepath.Join(base, "data"))
	root, data, err = appPaths()
	if err != nil || root != filepath.Join(base, "runtime-root") || data != filepath.Join(base, "data") {
		t.Fatalf("test overrides not honored: %s %s %v", root, data, err)
	}
	for _, name := range []string{"TRANSCRIBEME_RUNTIME_DIR", "TRANSCRIBEME_DATA_DIR"} {
		t.Run(name, func(t *testing.T) {
			t.Setenv(name, "relative")
			if _, _, err := appPaths(); err == nil || !strings.Contains(err.Error(), name) {
				t.Fatalf("accepted unsafe environment override: %v", err)
			}
		})
	}
}
