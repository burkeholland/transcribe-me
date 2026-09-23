package main

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"transcribeme/internal/transcribe"
)

//go:embed assets/runtime-manifest.json
var runtimeManifest []byte

type App struct {
	ctx      context.Context
	service  *transcribe.Service
	initErr  error
	initDone chan struct{}
	dialogMu sync.Mutex
	mediaMu  sync.Mutex
	media    map[string]bool
}

func NewApp(root, dataDir string) *App {
	return &App{
		service:  transcribe.NewService(root, dataDir, transcribe.RunProcess),
		initDone: make(chan struct{}),
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	go func() {
		defer close(a.initDone)
		if a.initErr != nil {
			runtime.LogError(ctx, a.initErr.Error())
			return
		}
		if err := a.service.Initialize(ctx, runtimeManifest); err != nil {
			runtime.LogError(ctx, err.Error())
		}
	}()
}

func (a *App) shutdown(ctx context.Context) {
	<-a.initDone
	a.service.Close()
}

func (a *App) beforeClose(ctx context.Context) bool {
	status := a.service.Status()
	if status.Job == nil || (status.Job.State != "preparing" && status.Job.State != "transcribing") {
		return false
	}
	a.dialogMu.Lock()
	defer a.dialogMu.Unlock()
	answer, err := runtime.MessageDialog(ctx, runtime.MessageDialogOptions{
		Type: runtime.QuestionDialog, Title: "Cancel transcription and close?",
		Message: "The current transcript is not finished. Closing will cancel it and remove the temporary audio. Your original video will not change.",
		Buttons: []string{"Yes", "No"}, DefaultButton: "No", CancelButton: "No",
	})
	if err != nil {
		runtime.LogError(ctx, err.Error())
		return true
	}
	return answer != "Yes"
}

func (a *App) Status() transcribe.Snapshot {
	if a.initErr != nil {
		return transcribe.Snapshot{
			SetupError: a.initErr.Error(), RuntimeState: "failed",
			Version: transcribe.Version, History: []transcribe.Summary{},
		}
	}
	return a.service.Status()
}

func (a *App) InstallModels() error {
	return a.service.InstallModels(a.ctx)
}

func (a *App) ChooseFile() (*transcribe.FileInfo, error) {
	a.dialogMu.Lock()
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Choose a video or audio file",
		Filters: []runtime.FileFilter{
			{DisplayName: "Video and audio", Pattern: "*.mp4;*.m4v;*.mov;*.mkv;*.webm;*.avi;*.wmv;*.mpeg;*.mpg;*.ts;*.mp3;*.wav;*.m4a;*.aac;*.flac;*.ogg;*.opus;*.wma"},
		},
	})
	a.dialogMu.Unlock()
	if err != nil || path == "" {
		return nil, err
	}
	info, err := a.InspectFile(path)
	if err != nil {
		return nil, err
	}
	return &info, nil
}

func (a *App) InspectFile(path string) (transcribe.FileInfo, error) {
	info, err := a.service.Inspect(a.ctx, path)
	if err != nil {
		return info, err
	}
	a.allowMedia(info.Path)
	return info, nil
}

// allowMedia records a file the user explicitly chose so mediaHandler may serve
// it back to the webview for the poster frame. Nothing else on disk is exposed.
func (a *App) allowMedia(path string) {
	clean, err := filepath.Abs(path)
	if err != nil {
		return
	}
	a.mediaMu.Lock()
	defer a.mediaMu.Unlock()
	if a.media == nil {
		a.media = make(map[string]bool)
	}
	if len(a.media) > 64 {
		a.media = make(map[string]bool)
	}
	a.media[strings.ToLower(clean)] = true
}

// mediaHandler streams a chosen file to the webview, which decodes one frame
// into the poster thumbnail. The local ffmpeg build is audio-only, so the webview
// is the only frame source available without shipping a full ffmpeg build.
func (a *App) mediaHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/media" {
			http.NotFound(w, r)
			return
		}
		clean, err := filepath.Abs(r.URL.Query().Get("path"))
		if err != nil {
			http.Error(w, "bad path", http.StatusBadRequest)
			return
		}
		a.mediaMu.Lock()
		ok := a.media[strings.ToLower(clean)]
		a.mediaMu.Unlock()
		if !ok {
			http.Error(w, "not allowed", http.StatusForbidden)
			return
		}
		file, err := os.Open(clean)
		if err != nil {
			http.Error(w, "unavailable", http.StatusNotFound)
			return
		}
		defer file.Close()
		stat, err := file.Stat()
		if err != nil || stat.IsDir() {
			http.Error(w, "unavailable", http.StatusNotFound)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		http.ServeContent(w, r, stat.Name(), stat.ModTime(), file)
	})
}

func (a *App) MinimiseWindow() {
	runtime.WindowMinimise(a.ctx)
}

func (a *App) ToggleMaximiseWindow() {
	runtime.WindowToggleMaximise(a.ctx)
}

func (a *App) CloseWindow() {
	runtime.Quit(a.ctx)
}

func (a *App) StartTranscription(path, language string) error {
	return a.service.Start(a.ctx, path, language)
}

func (a *App) Cancel() error {
	return a.service.Cancel()
}

func (a *App) GetTranscript(id string) (transcribe.Transcript, error) {
	record, err := a.service.Get(id)
	if err != nil {
		return record, err
	}
	// Reopening a saved transcript should show its poster too.
	if record.SourcePath != "" {
		a.allowMedia(record.SourcePath)
	}
	return record, nil
}

func (a *App) CopyTranscript(id string) error {
	t, err := a.service.Get(id)
	if err != nil {
		return err
	}
	return runtime.ClipboardSetText(a.ctx, t.Text)
}

func (a *App) ExportTranscript(id, format string) (string, error) {
	t, err := a.service.Get(id)
	if err != nil {
		return "", err
	}
	data, err := transcribe.Format(t, format)
	if err != nil {
		return "", err
	}
	a.dialogMu.Lock()
	defer a.dialogMu.Unlock()
	name := strings.TrimSuffix(t.FileName, filepath.Ext(t.FileName)) + "." + format
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title: "Export transcript", DefaultFilename: name,
		Filters: []runtime.FileFilter{{DisplayName: strings.ToUpper(format) + " file", Pattern: "*." + format}},
	})
	if err != nil || path == "" {
		return "", err
	}
	ext := filepath.Ext(path)
	if ext == "" {
		path += "." + format
	} else if !strings.EqualFold(ext, "."+format) {
		return "", fmt.Errorf("the filename must end in .%s", format)
	}
	if err := transcribe.WriteAtomic(path, data); err != nil {
		return "", err
	}
	return path, nil
}

func appPaths() (root, data string, err error) {
	exe, err := os.Executable()
	if err != nil {
		return "", "", err
	}
	root = filepath.Dir(exe)
	if override := os.Getenv("TRANSCRIBEME_RUNTIME_DIR"); override != "" {
		if !filepath.IsAbs(override) {
			return "", "", errors.New("TRANSCRIBEME_RUNTIME_DIR must be an absolute path")
		}
		root = override
	}
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		base, err = os.UserConfigDir()
		if err != nil {
			return "", "", err
		}
	}
	data = filepath.Join(base, "TranscribeMe")
	if override := os.Getenv("TRANSCRIBEME_DATA_DIR"); override != "" {
		if !filepath.IsAbs(override) {
			return "", "", errors.New("TRANSCRIBEME_DATA_DIR must be an absolute path")
		}
		data = override
	}
	return root, data, nil
}
