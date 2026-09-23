package transcribe

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
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
	p[0] = 'x'
	r.cancel()
	return 1, nil
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

func fakeModelClient(t *testing.T, source string, manifest Manifest, transform func(Asset, []byte) []byte) *http.Client {
	t.Helper()
	byURL := make(map[string]Asset)
	for _, asset := range selectAssets(manifest, runtimeModelAssets) {
		byURL[asset.URL] = asset
	}
	return &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		asset, ok := byURL[request.URL.String()]
		if !ok {
			return nil, errors.New("unexpected model URL")
		}
		data, err := os.ReadFile(filepath.Join(source, filepath.FromSlash(asset.Path)))
		if err != nil {
			return nil, err
		}
		if transform != nil {
			data = transform(asset, data)
		}
		return &http.Response{
			StatusCode:    http.StatusOK,
			Status:        "200 OK",
			Header:        make(http.Header),
			Body:          io.NopCloser(bytes.NewReader(data)),
			ContentLength: int64(len(data)),
			Request:       request,
		}, nil
	})}
}

func seedExistingModels(t *testing.T, source, target string, manifest Manifest) string {
	t.Helper()
	for _, asset := range selectAssets(manifest, runtimeModelAssets) {
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
	marker := filepath.Join(target, "runtime", "models", "existing-models.txt")
	if err := os.WriteFile(marker, []byte("keep existing models"), 0600); err != nil {
		t.Fatal(err)
	}
	return marker
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

func assertNoModelInstallArtifacts(t *testing.T, root string) {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".model-install-") || entry.Name() == ".models-backup" {
			t.Fatalf("model install artifact remains: %s", entry.Name())
		}
	}
}

func assertExistingModelsUnchanged(t *testing.T, target, marker string, manifest Manifest) {
	t.Helper()
	data, err := os.ReadFile(marker)
	if err != nil || string(data) != "keep existing models" {
		t.Fatalf("existing model marker changed: %q err=%v", data, err)
	}
	if err := VerifyModels(context.Background(), target, manifest); err != nil {
		t.Fatalf("existing models were damaged: %v", err)
	}
	assertNoModelInstallArtifacts(t, target)
}

func TestModelDownloadHost(t *testing.T) {
	for _, test := range []struct {
		host string
		want bool
	}{
		{host: "huggingface.co", want: true},
		{host: "HUGGINGFACE.CO", want: true},
		{host: "us.aws.cdn.hf.co", want: true},
		{host: "eu.cdn.hf.co", want: true},
		{host: "cdn.hf.co"},
		{host: "hf.co"},
		{host: "evil.huggingface.co"},
		{host: "us.aws.cdn.hf.co.example.invalid"},
		{host: ""},
	} {
		t.Run(test.host, func(t *testing.T) {
			if got := modelDownloadHost(test.host); got != test.want {
				t.Fatalf("modelDownloadHost(%q)=%v want %v", test.host, got, test.want)
			}
		})
	}
}

func TestModelRedirectPolicy(t *testing.T) {
	client := ModelHTTPClient()
	for _, test := range []struct {
		name    string
		target  string
		wantErr bool
	}{
		{name: "Hugging Face HTTPS", target: "https://huggingface.co/example/model"},
		{name: "Hugging Face CDN HTTPS", target: "https://us.aws.cdn.hf.co/example/model"},
		{name: "Hugging Face HTTP", target: "http://huggingface.co/example/model", wantErr: true},
		{name: "untrusted HTTPS", target: "https://example.com/model", wantErr: true},
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

func TestInstallModels(t *testing.T) {
	source, manifestData := fakeAssets(t)
	manifest := mustManifest(t, manifestData)
	target := t.TempDir()
	var states []string
	err := InstallModels(context.Background(), fakeModelClient(t, source, manifest, nil), target, manifest,
		func(state string, _, _ int64) { states = append(states, state) })
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyModels(context.Background(), target, manifest); err != nil {
		t.Fatal(err)
	}
	if !slicesContains(states, "downloading") || !slicesContains(states, "installing") {
		t.Fatalf("missing progress states: %v", states)
	}
}

func TestInstallModelsReplacesExistingModels(t *testing.T) {
	source, manifestData := fakeAssets(t)
	manifest := mustManifest(t, manifestData)
	target := t.TempDir()
	marker := seedExistingModels(t, source, target, manifest)

	if err := InstallModels(context.Background(), fakeModelClient(t, source, manifest, nil), target, manifest, nil); err != nil {
		t.Fatal(err)
	}
	if err := VerifyModels(context.Background(), target, manifest); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("old models were not replaced: %v", err)
	}
	assertNoModelInstallArtifacts(t, target)
}

func TestInstallModelsDamagedDownloadPreservesExistingModels(t *testing.T) {
	source, manifestData := fakeAssets(t)
	manifest := mustManifest(t, manifestData)
	target := t.TempDir()
	marker := seedExistingModels(t, source, target, manifest)
	client := fakeModelClient(t, source, manifest, func(asset Asset, data []byte) []byte {
		if strings.HasSuffix(asset.Path, "ggml-base.bin") {
			data = append([]byte(nil), data...)
			data[0] ^= 0xff
		}
		return data
	})

	err := InstallModels(context.Background(), client, target, manifest, nil)
	if err == nil || !strings.Contains(err.Error(), "SHA-256") {
		t.Fatalf("error=%v want damaged model rejection", err)
	}
	assertExistingModelsUnchanged(t, target, marker, manifest)
}

func TestInstallModelsCancellationPreservesExistingModels(t *testing.T) {
	source, manifestData := fakeAssets(t)
	manifest := mustManifest(t, manifestData)
	target := t.TempDir()
	marker := seedExistingModels(t, source, target, manifest)
	ctx, cancel := context.WithCancel(context.Background())
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode:    http.StatusOK,
			Status:        "200 OK",
			Header:        make(http.Header),
			Body:          &cancelAfterFirstRead{ctx: request.Context(), cancel: cancel},
			ContentLength: 7,
			Request:       request,
		}, nil
	})}

	err := InstallModels(ctx, client, target, manifest, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v want context cancellation", err)
	}
	assertExistingModelsUnchanged(t, target, marker, manifest)
}

func TestInstallModelsRejectsNonOKResponse(t *testing.T) {
	_, manifestData := fakeAssets(t)
	manifest := mustManifest(t, manifestData)
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusServiceUnavailable,
			Status:     "503 Service Unavailable",
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("unavailable")),
			Request:    request,
		}, nil
	})}
	target := t.TempDir()

	err := InstallModels(context.Background(), client, target, manifest, nil)
	if err == nil || !strings.Contains(err.Error(), "503 Service Unavailable") {
		t.Fatalf("error=%v want non-200 rejection", err)
	}
	assertNoModelInstallArtifacts(t, target)
}

func TestInstallModelsRejectsWrongContentLengthWithoutReadingBody(t *testing.T) {
	_, manifestData := fakeAssets(t)
	manifest := mustManifest(t, manifestData)
	body := &readTrackingBody{}
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode:    http.StatusOK,
			Status:        "200 OK",
			Header:        http.Header{"Content-Length": []string{strconv.FormatInt(8, 10)}},
			Body:          body,
			ContentLength: 8,
			Request:       request,
		}, nil
	})}
	target := t.TempDir()

	err := InstallModels(context.Background(), client, target, manifest, nil)
	if err == nil || !strings.Contains(err.Error(), "server reported 8") {
		t.Fatalf("error=%v want content-length rejection", err)
	}
	if body.reads != 0 {
		t.Fatalf("wrong-size response body was read %d times", body.reads)
	}
	assertNoModelInstallArtifacts(t, target)
}

func TestPublishedModels(t *testing.T) {
	target := os.Getenv("TRANSCRIBEME_TEST_MODEL_DOWNLOAD_DIR")
	if target == "" {
		t.Skip("set TRANSCRIBEME_TEST_MODEL_DOWNLOAD_DIR to test the published model sources")
	}
	if !filepath.IsAbs(target) {
		t.Fatal("TRANSCRIBEME_TEST_MODEL_DOWNLOAD_DIR must be an absolute dedicated test directory")
	}
	manifestData, err := os.ReadFile(filepath.Join("..", "..", "assets", "runtime-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	manifest := mustManifest(t, manifestData)
	if err := InstallModels(context.Background(), ModelHTTPClient(), target, manifest, nil); err != nil {
		t.Fatal(err)
	}
	if err := VerifyModels(context.Background(), target, manifest); err != nil {
		t.Fatal(err)
	}
}

func slicesContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
