package transcribe

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type Asset struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

type Manifest struct {
	Files []Asset `json:"files"`
}

type Engine struct {
	Root string
	Run  Runner
}

func (e Engine) tool(name string) string {
	switch name {
	case "whisper":
		return filepath.Join(e.Root, "runtime", "whisper", "whisper-cli.exe")
	case "model":
		return filepath.Join(e.Root, "runtime", "models", "ggml-base.bin")
	case "vad":
		return filepath.Join(e.Root, "runtime", "models", "ggml-silero-v5.1.2.bin")
	default:
		return filepath.Join(e.Root, "runtime", "ffmpeg", "bin", name+".exe")
	}
}

func VerifyAssets(ctx context.Context, root string, data []byte) error {
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return fmt.Errorf("read runtime manifest: %w", err)
	}
	required := map[string]bool{
		"runtime/whisper/whisper-cli.exe":       false,
		"runtime/ffmpeg/bin/ffmpeg.exe":         false,
		"runtime/ffmpeg/bin/ffprobe.exe":        false,
		"runtime/models/ggml-base.bin":          false,
		"runtime/models/ggml-silero-v5.1.2.bin": false,
	}
	for _, asset := range m.Files {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !filepath.IsLocal(asset.Path) || !strings.HasPrefix(asset.Path, "runtime/") {
			return errors.New("invalid runtime manifest path")
		}
		if err := verifyAsset(root, asset); err != nil {
			return fmt.Errorf("runtime is missing or damaged (%s): %w. Extract the complete download again", asset.Path, err)
		}
		if _, ok := required[asset.Path]; ok {
			required[asset.Path] = true
		}
	}
	for name, found := range required {
		if !found {
			return fmt.Errorf("runtime manifest is missing %s; run scripts\\fetch-runtime.ps1 and rebuild", name)
		}
	}
	return nil
}

func verifyAsset(root string, asset Asset) error {
	f, err := os.Open(filepath.Join(root, filepath.FromSlash(asset.Path)))
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	if !st.Mode().IsRegular() || st.Size() != asset.Size {
		return errors.New("unexpected file size or type")
	}
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	if hex.EncodeToString(h.Sum(nil)) != asset.SHA256 {
		return errors.New("SHA-256 mismatch")
	}
	return nil
}
