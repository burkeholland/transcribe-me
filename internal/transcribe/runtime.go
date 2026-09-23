package transcribe

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const maxRuntimeArchiveBytes int64 = 256 << 20

type RuntimeInstallProgress func(state string, downloaded, total int64)
type RuntimeInstaller func(context.Context, *http.Client, string, string, []byte, RuntimeInstallProgress) error

func RuntimeArchiveName() string {
	return fmt.Sprintf("TranscribeMe-%s-runtime-windows-x64.zip", Version)
}

func RuntimeArchiveURL() string {
	return fmt.Sprintf("https://github.com/burkeholland/transcribe-me/releases/download/v%s/%s", Version, RuntimeArchiveName())
}

func RuntimeHTTPClient() *http.Client {
	dialer := &net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}
	return &http.Client{
		Timeout: 20 * time.Minute,
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           dialer.DialContext,
			ForceAttemptHTTP2:     true,
			TLSHandshakeTimeout:   15 * time.Second,
			ResponseHeaderTimeout: 30 * time.Second,
			IdleConnTimeout:       90 * time.Second,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 8 {
				return errors.New("too many runtime download redirects")
			}
			if req.URL.Scheme != "https" || !runtimeDownloadHost(req.URL.Hostname()) {
				return fmt.Errorf("runtime download redirected to an untrusted address: %s", req.URL.Redacted())
			}
			return nil
		},
	}
}

func runtimeDownloadHost(host string) bool {
	return strings.EqualFold(host, "github.com") ||
		strings.EqualFold(host, "release-assets.githubusercontent.com")
}

func InstallRuntimeArchive(ctx context.Context, client *http.Client, root, source string, manifestData []byte, progress RuntimeInstallProgress) error {
	manifest, err := ParseManifest(manifestData)
	if err != nil {
		return err
	}
	if client == nil {
		client = RuntimeHTTPClient()
	}
	if progress == nil {
		progress = func(string, int64, int64) {}
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return fmt.Errorf("create application data folder: %w", err)
	}
	download, err := os.CreateTemp(root, ".runtime-download-*.zip")
	if err != nil {
		return fmt.Errorf("create runtime download: %w", err)
	}
	downloadPath := download.Name()
	defer os.Remove(downloadPath)
	defer download.Close()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return fmt.Errorf("prepare runtime download: %w", err)
	}
	request.Header.Set("User-Agent", "TranscribeMe/"+Version)
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("download local transcription engine: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("download local transcription engine: server returned %s", response.Status)
	}
	if response.ContentLength > maxRuntimeArchiveBytes {
		return fmt.Errorf("download local transcription engine: file is larger than %d MiB", maxRuntimeArchiveBytes>>20)
	}
	total := response.ContentLength
	progress("downloading", 0, total)
	reader := &runtimeProgressReader{reader: response.Body, total: total, report: progress}
	written, err := io.Copy(download, io.LimitReader(reader, maxRuntimeArchiveBytes+1))
	if err != nil {
		return fmt.Errorf("download local transcription engine: %w", err)
	}
	if written > maxRuntimeArchiveBytes {
		return fmt.Errorf("download local transcription engine: file exceeded %d MiB", maxRuntimeArchiveBytes>>20)
	}
	if response.ContentLength >= 0 && written != response.ContentLength {
		return fmt.Errorf("download local transcription engine: expected %d bytes, received %d", response.ContentLength, written)
	}
	if err := download.Sync(); err != nil {
		return fmt.Errorf("save runtime download: %w", err)
	}
	if err := download.Close(); err != nil {
		return fmt.Errorf("close runtime download: %w", err)
	}
	progress("installing", written, total)

	stage, err := os.MkdirTemp(root, ".runtime-install-*")
	if err != nil {
		return fmt.Errorf("create runtime staging folder: %w", err)
	}
	defer os.RemoveAll(stage)
	if err := extractRuntimeArchive(ctx, downloadPath, stage, manifest); err != nil {
		return err
	}
	if err := VerifyManifest(ctx, stage, manifest); err != nil {
		return fmt.Errorf("verify downloaded runtime: %w", err)
	}
	return activateRuntime(root, stage)
}

type runtimeProgressReader struct {
	reader     io.Reader
	total      int64
	downloaded int64
	lastReport int64
	report     RuntimeInstallProgress
}

func (r *runtimeProgressReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.downloaded += int64(n)
	if r.downloaded-r.lastReport >= 512<<10 || err == io.EOF {
		r.lastReport = r.downloaded
		r.report("downloading", r.downloaded, r.total)
	}
	return n, err
}

func extractRuntimeArchive(ctx context.Context, archivePath, stage string, manifest Manifest) error {
	archive, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("open runtime archive: %w", err)
	}
	defer archive.Close()
	expected := make(map[string]Asset, len(manifest.Files))
	for _, asset := range manifest.Files {
		expected[asset.Path] = asset
	}
	seen := make(map[string]bool, len(expected))
	for _, entry := range archive.File {
		if err := ctx.Err(); err != nil {
			return err
		}
		name := strings.TrimSuffix(strings.ReplaceAll(entry.Name, "\\", "/"), "/")
		if name == "" || entry.FileInfo().IsDir() {
			continue
		}
		asset, ok := expected[name]
		if !ok || seen[name] || !filepath.IsLocal(filepath.FromSlash(name)) || entry.FileInfo().Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("runtime archive contains an unexpected entry: %s", entry.Name)
		}
		if entry.UncompressedSize64 != uint64(asset.Size) {
			return fmt.Errorf("runtime archive entry has the wrong size: %s", name)
		}
		target := filepath.Join(stage, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			return fmt.Errorf("create runtime folder: %w", err)
		}
		input, err := entry.Open()
		if err != nil {
			return fmt.Errorf("open runtime archive entry %s: %w", name, err)
		}
		output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			input.Close()
			return fmt.Errorf("create runtime file %s: %w", name, err)
		}
		hash := sha256.New()
		written, copyErr := io.Copy(io.MultiWriter(output, hash), contextReader{ctx: ctx, reader: input})
		closeErr := errors.Join(input.Close(), output.Close())
		if copyErr != nil || closeErr != nil {
			return fmt.Errorf("extract runtime file %s: %w", name, errors.Join(copyErr, closeErr))
		}

		if written != asset.Size || hex.EncodeToString(hash.Sum(nil)) != asset.SHA256 {
			return fmt.Errorf("runtime archive entry failed verification: %s", name)
		}
		seen[name] = true
	}
	for name := range expected {
		if !seen[name] {
			return fmt.Errorf("runtime archive is missing %s", name)
		}
	}
	return nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

func recoverRuntimeBackup(ctx context.Context, root string, manifest Manifest) error {
	target := filepath.Join(root, "runtime")
	backup := filepath.Join(root, ".runtime-backup")
	if _, err := os.Lstat(target); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("recover interrupted runtime update: inspect runtime target: %w", err)
	}
	if _, err := os.Lstat(backup); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("recover interrupted runtime update: inspect runtime backup: %w", err)
	}
	if err := os.Rename(backup, target); err != nil {
		return fmt.Errorf("recover interrupted runtime update: restore runtime backup: %w", err)
	}
	if err := VerifyManifest(ctx, root, manifest); err != nil {
		if rollbackErr := os.Rename(target, backup); rollbackErr != nil {
			return fmt.Errorf("recover interrupted runtime update: %w", errors.Join(
				fmt.Errorf("runtime backup is invalid: %w", err),
				fmt.Errorf("restore invalid backup location: %w", rollbackErr),
			))
		}
		return fmt.Errorf("recover interrupted runtime update: runtime backup is invalid: %w", err)
	}
	return nil
}

func cleanInterruptedRuntimeInstall(root string) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		return fmt.Errorf("clean interrupted runtime install: read application data folder: %w", err)
	}
	isTempName := func(name, prefix, suffix string) bool {
		if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, suffix) {
			return false
		}
		randomEnd := len(name) - len(suffix)
		if randomEnd <= len(prefix) || randomEnd-len(prefix) > 10 {
			return false
		}
		for _, char := range name[len(prefix):randomEnd] {
			if char < '0' || char > '9' {
				return false
			}
		}
		return true
	}

	var cleanupErr error
	for _, entry := range entries {
		name := entry.Name()
		path := filepath.Join(root, name)
		switch {
		case entry.Type().IsRegular() && isTempName(name, ".runtime-download-", ".zip"):
			if err := os.Remove(path); err != nil {
				cleanupErr = errors.Join(cleanupErr, fmt.Errorf("remove orphaned runtime download %s: %w", name, err))
			}
		case entry.IsDir() && isTempName(name, ".runtime-install-", ""):
			if err := os.RemoveAll(path); err != nil {
				cleanupErr = errors.Join(cleanupErr, fmt.Errorf("remove orphaned runtime staging folder %s: %w", name, err))
			}
		}
	}
	if cleanupErr != nil {
		return fmt.Errorf("clean interrupted runtime install: %w", cleanupErr)
	}
	return nil
}

func cleanReplacedRuntimeBackup(root string) error {
	if err := os.RemoveAll(filepath.Join(root, ".runtime-backup")); err != nil {
		return fmt.Errorf("remove replaced runtime backup: %w", err)
	}
	return nil
}

func activateRuntime(root, stage string) error {
	stagedRuntime := filepath.Join(stage, "runtime")
	target := filepath.Join(root, "runtime")
	backup := filepath.Join(root, ".runtime-backup")
	if err := os.RemoveAll(backup); err != nil {
		return fmt.Errorf("remove old runtime backup: %w", err)
	}
	hadRuntime := false
	if _, err := os.Stat(target); err == nil {
		hadRuntime = true
		if err := os.Rename(target, backup); err != nil {
			return fmt.Errorf("prepare existing runtime for replacement: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect existing runtime: %w", err)
	}
	if err := os.Rename(stagedRuntime, target); err != nil {
		if hadRuntime {
			_ = os.Rename(backup, target)
		}
		return fmt.Errorf("activate downloaded runtime: %w", err)
	}
	if err := os.RemoveAll(backup); err != nil {
		return fmt.Errorf("remove replaced runtime: %w", err)
	}
	return nil
}
