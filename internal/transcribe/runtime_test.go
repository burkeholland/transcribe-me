package transcribe

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

type cancelAfterFirstRead struct {
	ctx    context.Context
	cancel context.CancelFunc
	read   bool
}

func (r *cancelAfterFirstRead) Read(p []byte) (int, error) {
	if r.read {
		return 0, r.ctx.Err()
	}
	r.read = true
	n := copy(p, []byte("partial runtime archive"))
	r.cancel()
	return n, nil
}

func (*cancelAfterFirstRead) Close() error {
	return nil
}

type readTrackingBody struct {
	reads int
}

func (b *readTrackingBody) Read([]byte) (int, error) {
	b.reads++
	return 0, io.EOF
}

func (*readTrackingBody) Close() error {
	return nil
}

func runtimeArchive(t *testing.T, root string, manifest Manifest, extra string, damage bool) []byte {
	t.Helper()
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	for index, asset := range manifest.Files {
		header := &zip.FileHeader{Name: asset.Path, Method: zip.Deflate}
		header.SetMode(0600)
		entry, err := archive.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(asset.Path)))
		if err != nil {
			t.Fatal(err)
		}
		if damage && index == 0 {
			data[0] ^= 0xff
		}
		if _, err := entry.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if extra != "" {
		entry, err := archive.Create(extra)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte("unexpected")); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func serveRuntime(t *testing.T, body []byte) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)
	return server
}

func seedExistingRuntime(t *testing.T, source, target string, manifest Manifest) string {
	t.Helper()
	for _, asset := range manifest.Files {
		data, err := os.ReadFile(filepath.Join(source, filepath.FromSlash(asset.Path)))
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(target, filepath.FromSlash(asset.Path))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	marker := filepath.Join(target, "runtime", "existing-runtime.txt")
	if err := os.WriteFile(marker, []byte("keep existing runtime"), 0600); err != nil {
		t.Fatal(err)
	}
	return marker
}

func assertNoRuntimeInstallArtifacts(t *testing.T, root string) {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".runtime-download-") ||
			strings.HasPrefix(name, ".runtime-install-") ||
			name == ".runtime-backup" {
			t.Fatalf("runtime install artifact remains: %s", name)
		}
	}
}

func assertExistingRuntimeUnchanged(t *testing.T, target, marker string, manifestData []byte) {
	t.Helper()
	data, err := os.ReadFile(marker)
	if err != nil || string(data) != "keep existing runtime" {
		t.Fatalf("existing runtime marker changed: %q err=%v", data, err)
	}
	if err := VerifyAssets(context.Background(), target, manifestData); err != nil {
		t.Fatalf("existing runtime was damaged: %v", err)
	}
	assertNoRuntimeInstallArtifacts(t, target)
}

func TestRuntimeDownloadHost(t *testing.T) {
	for _, test := range []struct {
		host string
		want bool
	}{
		{host: "github.com", want: true},
		{host: "GitHub.com", want: true},
		{host: "release-assets.githubusercontent.com", want: true},
		{host: "RELEASE-ASSETS.GITHUBUSERCONTENT.COM", want: true},
		{host: "githubusercontent.com"},
		{host: "objects.githubusercontent.com"},
		{host: "user-images.githubusercontent.com"},
		{host: "evil.github.com"},
		{host: "github.com.example.invalid"},
		{host: "release-assets.githubusercontent.com.example.invalid"},
		{host: "release-assets.githubusercontent.com."},
		{host: ""},
	} {
		t.Run(test.host, func(t *testing.T) {
			if got := runtimeDownloadHost(test.host); got != test.want {
				t.Fatalf("runtimeDownloadHost(%q)=%v want %v", test.host, got, test.want)
			}
		})
	}
}

func TestRuntimeRedirectPolicy(t *testing.T) {
	client := RuntimeHTTPClient()
	for _, test := range []struct {
		name    string
		target  string
		wantErr bool
	}{
		{name: "github HTTPS", target: "https://github.com/example/runtime.zip"},
		{name: "release asset HTTPS", target: "https://release-assets.githubusercontent.com/example/runtime.zip"},
		{name: "github HTTP", target: "http://github.com/example/runtime.zip", wantErr: true},
		{name: "untrusted HTTPS", target: "https://objects.githubusercontent.com/example/runtime.zip", wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			request, err := http.NewRequest(http.MethodGet, test.target, nil)
			if err != nil {
				t.Fatal(err)
			}
			err = client.CheckRedirect(request, nil)
			if (err != nil) != test.wantErr {
				t.Fatalf("redirect to %s returned %v", test.target, err)
			}
		})
	}
}

func TestInstallRuntimeArchive(t *testing.T) {
	source, manifestData := fakeAssets(t)
	manifest := mustManifest(t, manifestData)
	server := serveRuntime(t, runtimeArchive(t, source, manifest, "", false))
	target := t.TempDir()
	var states []string
	err := InstallRuntimeArchive(context.Background(), server.Client(), target, server.URL, manifestData,
		func(state string, _, _ int64) { states = append(states, state) })
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyAssets(context.Background(), target, manifestData); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(states, "downloading") || !slices.Contains(states, "installing") {
		t.Fatalf("missing progress states: %v", states)
	}
}

func TestInstallRuntimeReplacesExistingRuntime(t *testing.T) {
	source, manifestData := fakeAssets(t)
	manifest := mustManifest(t, manifestData)
	target := t.TempDir()
	marker := seedExistingRuntime(t, source, target, manifest)
	server := serveRuntime(t, runtimeArchive(t, source, manifest, "", false))

	if err := InstallRuntimeArchive(context.Background(), server.Client(), target, server.URL, manifestData, nil); err != nil {
		t.Fatal(err)
	}
	if err := VerifyAssets(context.Background(), target, manifestData); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("old runtime was not replaced: %v", err)
	}
	assertNoRuntimeInstallArtifacts(t, target)
}

func TestInstallRuntimeDamagedArchivePreservesExistingRuntime(t *testing.T) {
	source, manifestData := fakeAssets(t)
	manifest := mustManifest(t, manifestData)
	target := t.TempDir()
	marker := seedExistingRuntime(t, source, target, manifest)
	server := serveRuntime(t, runtimeArchive(t, source, manifest, "", true))

	err := InstallRuntimeArchive(context.Background(), server.Client(), target, server.URL, manifestData, nil)
	if err == nil || !strings.Contains(err.Error(), "failed verification") {
		t.Fatalf("error=%v want damaged archive rejection", err)
	}
	assertExistingRuntimeUnchanged(t, target, marker, manifestData)
}

func TestInstallRuntimeCancellationPreservesExistingRuntime(t *testing.T) {
	source, manifestData := fakeAssets(t)
	manifest := mustManifest(t, manifestData)
	target := t.TempDir()
	marker := seedExistingRuntime(t, source, target, manifest)
	ctx, cancel := context.WithCancel(context.Background())
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode:    http.StatusOK,
			Status:        "200 OK",
			Header:        make(http.Header),
			Body:          &cancelAfterFirstRead{ctx: request.Context(), cancel: cancel},
			ContentLength: 1024,
			Request:       request,
		}, nil
	})}

	err := InstallRuntimeArchive(ctx, client, target, "https://github.com/example/runtime.zip", manifestData, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v want context cancellation", err)
	}
	assertExistingRuntimeUnchanged(t, target, marker, manifestData)
}

func TestInstallRuntimeRejectsNonOKResponse(t *testing.T) {
	_, manifestData := fakeAssets(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)
	target := t.TempDir()

	err := InstallRuntimeArchive(context.Background(), server.Client(), target, server.URL, manifestData, nil)
	if err == nil || !strings.Contains(err.Error(), "503 Service Unavailable") {
		t.Fatalf("error=%v want non-200 rejection", err)
	}
	assertNoRuntimeInstallArtifacts(t, target)
}

func TestInstallRuntimeRejectsOversizedContentLengthWithoutReadingBody(t *testing.T) {
	_, manifestData := fakeAssets(t)
	body := &readTrackingBody{}
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode:    http.StatusOK,
			Status:        "200 OK",
			Header:        http.Header{"Content-Length": []string{strconv.FormatInt(maxRuntimeArchiveBytes+1, 10)}},
			Body:          body,
			ContentLength: maxRuntimeArchiveBytes + 1,
			Request:       request,
		}, nil
	})}
	target := t.TempDir()

	err := InstallRuntimeArchive(context.Background(), client, target, "https://github.com/example/runtime.zip", manifestData, nil)
	if err == nil || !strings.Contains(err.Error(), "file is larger") {
		t.Fatalf("error=%v want oversized response rejection", err)
	}
	if body.reads != 0 {
		t.Fatalf("oversized response body was read %d times", body.reads)
	}
	assertNoRuntimeInstallArtifacts(t, target)
}

func TestInstallRuntimeRejectsDamagedOrUnexpectedArchives(t *testing.T) {
	source, manifestData := fakeAssets(t)
	manifest := mustManifest(t, manifestData)
	for _, test := range []struct {
		name   string
		extra  string
		damage bool
		want   string
	}{
		{name: "damaged", damage: true, want: "failed verification"},
		{name: "extra entry", extra: "runtime/unexpected.exe", want: "unexpected entry"},
		{name: "path traversal", extra: "../outside.txt", want: "unexpected entry"},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := serveRuntime(t, runtimeArchive(t, source, manifest, test.extra, test.damage))
			target := t.TempDir()
			err := InstallRuntimeArchive(context.Background(), server.Client(), target, server.URL, manifestData, nil)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error=%v want %q", err, test.want)
			}
			if _, statErr := os.Stat(filepath.Join(target, "runtime")); !os.IsNotExist(statErr) {
				t.Fatalf("failed archive installed files: %v", statErr)
			}
		})
	}
}

func TestPackagedRuntimeArchive(t *testing.T) {
	path := os.Getenv("TRANSCRIBEME_TEST_RUNTIME_ARCHIVE")
	if path == "" {
		t.Skip("set TRANSCRIBEME_TEST_RUNTIME_ARCHIVE to verify a production runtime ZIP")
	}
	if !filepath.IsAbs(path) {
		t.Fatal("TRANSCRIBEME_TEST_RUNTIME_ARCHIVE must be an absolute path")
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	stat, err := file.Stat()
	if err != nil {
		file.Close()
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", strconv.FormatInt(stat.Size(), 10))
		_, _ = file.Seek(0, 0)
		_, _ = io.Copy(w, file)
	}))
	t.Cleanup(func() {
		server.Close()
		file.Close()
	})
	manifest, err := os.ReadFile(filepath.Join("..", "..", "assets", "runtime-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	target := os.Getenv("TRANSCRIBEME_TEST_RUNTIME_INSTALL_DIR")
	if target == "" {
		target = t.TempDir()
	} else if !filepath.IsAbs(target) {
		t.Fatal("TRANSCRIBEME_TEST_RUNTIME_INSTALL_DIR must be an absolute dedicated test directory")
	}
	if err := InstallRuntimeArchive(context.Background(), server.Client(), target, server.URL, manifest, nil); err != nil {
		t.Fatal(err)
	}
	if err := VerifyAssets(context.Background(), target, manifest); err != nil {
		t.Fatal(err)
	}
}
