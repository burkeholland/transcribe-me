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

var requiredRuntimeAssets = map[string]bool{
	"runtime/whisper/whisper-cli.exe":       false,
	"runtime/ffmpeg/bin/ffmpeg.exe":         false,
	"runtime/ffmpeg/bin/ffprobe.exe":        false,
	"runtime/models/ggml-base.bin":          false,
	"runtime/models/ggml-silero-v5.1.2.bin": false,
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
	m, err := ParseManifest(data)
	if err != nil {
		return err
	}
	return VerifyManifest(ctx, root, m)
}

func ParseManifest(data []byte) (Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return m, fmt.Errorf("read runtime manifest: %w", err)
	}
	found := make(map[string]bool, len(requiredRuntimeAssets))
	for name := range requiredRuntimeAssets {
		found[name] = false
	}
	for _, asset := range m.Files {
		if !filepath.IsLocal(filepath.FromSlash(asset.Path)) || !strings.HasPrefix(asset.Path, "runtime/") ||
			asset.Size <= 0 {
			return m, errors.New("invalid runtime manifest entry")
		}
		hash, err := hex.DecodeString(asset.SHA256)
		if err != nil || len(hash) != sha256.Size || hex.EncodeToString(hash) != asset.SHA256 {
			return m, errors.New("invalid runtime manifest entry")
		}
		if _, duplicate := found[asset.Path]; duplicate && found[asset.Path] {
			return m, fmt.Errorf("duplicate runtime manifest path: %s", asset.Path)
		}
		if _, ok := found[asset.Path]; !ok {
			return m, fmt.Errorf("unexpected runtime manifest path: %s", asset.Path)
		}
		found[asset.Path] = true
	}
	for name, present := range found {
		if !present {
			return m, fmt.Errorf("runtime manifest is missing %s", name)
		}
	}
	return m, nil
}

func VerifyManifest(ctx context.Context, root string, manifest Manifest) error {
	for _, asset := range manifest.Files {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := verifyAsset(root, asset); err != nil {
			return fmt.Errorf("runtime is missing or damaged (%s): %w", asset.Path, err)
		}
	}
	return nil
}

func RuntimeInstalledSize(manifest Manifest) int64 {
	var total int64
	for _, asset := range manifest.Files {
		total += asset.Size
	}
	return total
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
