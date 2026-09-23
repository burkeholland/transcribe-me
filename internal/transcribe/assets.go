package transcribe

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

type Asset struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
	URL    string `json:"url,omitempty"`
}

type Manifest struct {
	Files []Asset `json:"files"`
}

type Engine struct {
	Root      string
	ModelRoot string
	Run       Runner
}

var runtimeToolAssets = map[string]bool{
	"runtime/whisper/whisper-cli.exe": false,
	"runtime/ffmpeg/bin/ffmpeg.exe":   false,
	"runtime/ffmpeg/bin/ffprobe.exe":  false,
}

var runtimeModelAssets = map[string]bool{
	"runtime/models/ggml-base.bin":          false,
	"runtime/models/ggml-silero-v5.1.2.bin": false,
}

func (e Engine) tool(name string) string {
	modelRoot := e.ModelRoot
	if modelRoot == "" {
		modelRoot = e.Root
	}
	switch name {
	case "whisper":
		return filepath.Join(e.Root, "runtime", "whisper", "whisper-cli.exe")
	case "model":
		return filepath.Join(modelRoot, "runtime", "models", "ggml-base.bin")
	case "vad":
		return filepath.Join(modelRoot, "runtime", "models", "ggml-silero-v5.1.2.bin")
	default:
		return filepath.Join(e.Root, "runtime", "ffmpeg", "bin", name+".exe")
	}
}

func ParseManifest(data []byte) (Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return m, fmt.Errorf("read runtime manifest: %w", err)
	}
	found := make(map[string]bool, len(runtimeToolAssets)+len(runtimeModelAssets))
	for name := range runtimeToolAssets {
		found[name] = false
	}
	for name := range runtimeModelAssets {
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
		if _, model := runtimeModelAssets[asset.Path]; model {
			if !validModelURL(asset.URL, filepath.Base(asset.Path)) {
				return m, fmt.Errorf("invalid model source for %s", asset.Path)
			}
		} else if asset.URL != "" {
			return m, fmt.Errorf("native runtime asset must not have a download URL: %s", asset.Path)
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

func validModelURL(raw, fileName string) bool {
	source, err := url.Parse(raw)
	if err != nil || source.Scheme != "https" || !strings.EqualFold(source.Host, "huggingface.co") ||
		source.User != nil || source.RawQuery != "" || source.Fragment != "" {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(source.Path, "/"), "/")
	if len(parts) != 5 || parts[2] != "resolve" || parts[4] != fileName {
		return false
	}
	revision, err := hex.DecodeString(parts[3])
	return err == nil && len(revision) == 20 && hex.EncodeToString(revision) == parts[3]
}

func VerifyManifest(ctx context.Context, root string, manifest Manifest) error {
	return verifyManifestAssets(ctx, root, manifest.Files)
}

func VerifyTools(ctx context.Context, root string, manifest Manifest) error {
	return verifyManifestAssets(ctx, root, selectAssets(manifest, runtimeToolAssets))
}

func VerifyModels(ctx context.Context, root string, manifest Manifest) error {
	return verifyManifestAssets(ctx, root, selectAssets(manifest, runtimeModelAssets))
}

func selectAssets(manifest Manifest, selected map[string]bool) []Asset {
	assets := make([]Asset, 0, len(selected))
	for _, asset := range manifest.Files {
		if _, ok := selected[asset.Path]; ok {
			assets = append(assets, asset)
		}
	}
	return assets
}

func verifyManifestAssets(ctx context.Context, root string, assets []Asset) error {
	for _, asset := range assets {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := verifyAsset(root, asset); err != nil {
			return fmt.Errorf("runtime is missing or damaged (%s): %w", asset.Path, err)
		}
	}
	return nil
}

func ModelInstalledSize(manifest Manifest) int64 {
	var total int64
	for _, asset := range manifest.Files {
		if _, ok := runtimeModelAssets[asset.Path]; ok {
			total += asset.Size
		}
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
