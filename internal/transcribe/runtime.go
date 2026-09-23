package transcribe

import (
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

type RuntimeInstallProgress func(state string, downloaded, total int64)
type ModelInstaller func(context.Context, *http.Client, string, Manifest, RuntimeInstallProgress) error

type modelActivationCleanupError struct {
	err error
}

func (e *modelActivationCleanupError) Error() string {
	return e.err.Error()
}

func (e *modelActivationCleanupError) Unwrap() error {
	return e.err
}

func ModelHTTPClient() *http.Client {
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
				return errors.New("too many model download redirects")
			}
			if req.URL.Scheme != "https" || !modelDownloadHost(req.URL.Hostname()) {
				return fmt.Errorf("model download redirected to an untrusted address: %s", req.URL.Redacted())
			}
			return nil
		},
	}
}

func modelDownloadHost(host string) bool {
	host = strings.ToLower(host)
	return host == "huggingface.co" || strings.HasSuffix(host, ".cdn.hf.co")
}

func InstallModels(ctx context.Context, client *http.Client, root string, manifest Manifest, progress RuntimeInstallProgress) error {
	if client == nil {
		client = ModelHTTPClient()
	}
	if progress == nil {
		progress = func(string, int64, int64) {}
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return fmt.Errorf("create application data folder: %w", err)
	}
	stage, err := os.MkdirTemp(root, ".model-install-*")
	if err != nil {
		return fmt.Errorf("create model staging folder: %w", err)
	}
	defer os.RemoveAll(stage)
	if err := os.MkdirAll(filepath.Join(stage, "runtime", "models"), 0700); err != nil {
		return fmt.Errorf("create model staging directory: %w", err)
	}

	models := selectAssets(manifest, runtimeModelAssets)
	total := ModelInstalledSize(manifest)
	var downloaded int64
	progress("downloading", 0, total)
	for _, asset := range models {
		target := filepath.Join(stage, filepath.FromSlash(asset.Path))
		written, err := downloadModel(ctx, client, asset, target, downloaded, total, progress)
		if err != nil {
			return err
		}
		downloaded += written
	}
	progress("installing", downloaded, total)
	return activateModels(root, stage)
}

func downloadModel(
	ctx context.Context,
	client *http.Client,
	asset Asset,
	target string,
	completed int64,
	total int64,
	progress RuntimeInstallProgress,
) (int64, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.URL, nil)
	if err != nil {
		return 0, fmt.Errorf("prepare model download %s: %w", filepath.Base(asset.Path), err)
	}
	request.Header.Set("Accept-Encoding", "identity")
	request.Header.Set("User-Agent", "TranscribeMe/"+Version)
	response, err := client.Do(request)
	if err != nil {
		return 0, fmt.Errorf("download model %s: %w", filepath.Base(asset.Path), err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("download model %s: server returned %s", filepath.Base(asset.Path), response.Status)
	}
	if response.ContentLength >= 0 && response.ContentLength != asset.Size {
		return 0, fmt.Errorf(
			"download model %s: expected %d bytes, server reported %d",
			filepath.Base(asset.Path), asset.Size, response.ContentLength,
		)
	}
	output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return 0, fmt.Errorf("create model file %s: %w", filepath.Base(asset.Path), err)
	}
	hash := sha256.New()
	reader := &modelProgressReader{
		reader: response.Body, base: completed, total: total, report: progress,
	}
	written, copyErr := io.Copy(io.MultiWriter(output, hash), io.LimitReader(contextReader{ctx: ctx, reader: reader}, asset.Size+1))
	syncErr := output.Sync()
	closeErr := output.Close()
	if err := errors.Join(copyErr, syncErr, closeErr); err != nil {
		return 0, fmt.Errorf("save model %s: %w", filepath.Base(asset.Path), err)
	}
	if written != asset.Size {
		return 0, fmt.Errorf("download model %s: expected %d bytes, received %d", filepath.Base(asset.Path), asset.Size, written)
	}
	if hex.EncodeToString(hash.Sum(nil)) != asset.SHA256 {
		return 0, fmt.Errorf("download model %s: SHA-256 verification failed", filepath.Base(asset.Path))
	}
	progress("downloading", completed+written, total)
	return written, nil
}

type modelProgressReader struct {
	reader     io.Reader
	base       int64
	total      int64
	downloaded int64
	lastReport int64
	report     RuntimeInstallProgress
}

func (r *modelProgressReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.downloaded += int64(n)
	if r.downloaded-r.lastReport >= 512<<10 || err == io.EOF {
		r.lastReport = r.downloaded
		r.report("downloading", r.base+r.downloaded, r.total)
	}
	return n, err
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
	return recoverBackup(
		filepath.Join(root, "runtime"),
		filepath.Join(root, ".runtime-backup"),
		"runtime",
		func() error { return VerifyManifest(ctx, root, manifest) },
	)
}

func recoverModelsBackup(ctx context.Context, root string, manifest Manifest) error {
	target := filepath.Join(root, "runtime", "models")
	if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		return fmt.Errorf("recover interrupted model update: create runtime folder: %w", err)
	}
	return recoverBackup(
		target,
		filepath.Join(root, ".models-backup"),
		"model",
		func() error { return VerifyModels(ctx, root, manifest) },
	)
}

func recoverBackup(target, backup, label string, verify func() error) error {
	if _, err := os.Lstat(target); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("recover interrupted %s update: inspect target: %w", label, err)
	}
	if _, err := os.Lstat(backup); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("recover interrupted %s update: inspect backup: %w", label, err)
	}
	if err := os.Rename(backup, target); err != nil {
		return fmt.Errorf("recover interrupted %s update: restore backup: %w", label, err)
	}
	if err := verify(); err != nil {
		if rollbackErr := os.Rename(target, backup); rollbackErr != nil {
			return fmt.Errorf("recover interrupted %s update: %w", label, errors.Join(
				fmt.Errorf("backup is invalid: %w", err),
				fmt.Errorf("restore invalid backup location: %w", rollbackErr),
			))
		}
		return fmt.Errorf("recover interrupted %s update: backup is invalid: %w", label, err)
	}
	return nil
}

func cleanInterruptedRuntimeInstall(root string) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		return fmt.Errorf("clean interrupted runtime install: read application data folder: %w", err)
	}
	var cleanupErr error
	for _, entry := range entries {
		name := entry.Name()
		path := filepath.Join(root, name)
		switch {
		case entry.Type().IsRegular() && isGoTempName(name, ".runtime-download-", ".zip"):
			if err := os.Remove(path); err != nil {
				cleanupErr = errors.Join(cleanupErr, fmt.Errorf("remove orphaned runtime download %s: %w", name, err))
			}
		case entry.IsDir() && (isGoTempName(name, ".runtime-install-", "") || isGoTempName(name, ".model-install-", "")):
			if err := os.RemoveAll(path); err != nil {
				cleanupErr = errors.Join(cleanupErr, fmt.Errorf("remove orphaned model staging folder %s: %w", name, err))
			}
		}
	}
	if cleanupErr != nil {
		return fmt.Errorf("clean interrupted runtime install: %w", cleanupErr)
	}
	return nil
}

func isGoTempName(name, prefix, suffix string) bool {
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

func cleanReplacedRuntimeBackup(root string) error {
	return removeBackup(root, ".runtime-backup", "runtime")
}

func cleanReplacedModelsBackup(root string) error {
	return removeBackup(root, ".models-backup", "model")
}

func removeBackup(root, name, label string) error {
	if err := os.RemoveAll(filepath.Join(root, name)); err != nil {
		return fmt.Errorf("remove replaced %s backup: %w", label, err)
	}
	return nil
}

func activateModels(root, stage string) error {
	stagedModels := filepath.Join(stage, "runtime", "models")
	target := filepath.Join(root, "runtime", "models")
	backup := filepath.Join(root, ".models-backup")
	if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		return fmt.Errorf("create runtime model folder: %w", err)
	}
	if err := os.RemoveAll(backup); err != nil {
		return fmt.Errorf("remove old model backup: %w", err)
	}
	hadModels := false
	if _, err := os.Stat(target); err == nil {
		hadModels = true
		if err := os.Rename(target, backup); err != nil {
			return fmt.Errorf("prepare existing models for replacement: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect existing models: %w", err)
	}
	if err := os.Rename(stagedModels, target); err != nil {
		if hadModels {
			if rollbackErr := os.Rename(backup, target); rollbackErr != nil {
				return fmt.Errorf("activate downloaded models: %w", errors.Join(err, fmt.Errorf("restore previous models: %w", rollbackErr)))
			}
		}
		return fmt.Errorf("activate downloaded models: %w", err)
	}
	if err := os.RemoveAll(backup); err != nil {
		return &modelActivationCleanupError{err: fmt.Errorf("remove replaced models: %w", err)}
	}
	return nil
}
