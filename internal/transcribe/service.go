package transcribe

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Service struct {
	mu            sync.Mutex
	engine        Engine
	packageRoot   string
	store         Store
	dataDir       string
	snapshot      Snapshot
	cancel        context.CancelFunc
	done          chan struct{}
	manifest      Manifest
	install       ModelInstaller
	client        *http.Client
	installCancel context.CancelFunc
	installDone   chan struct{}
}

func NewService(root, dataDir string, runner Runner) *Service {
	return &Service{
		engine: Engine{Root: root, Run: runner}, packageRoot: root, dataDir: dataDir,
		store: Store{Dir: filepath.Join(dataDir, "transcripts")},
		snapshot: Snapshot{
			ModelName: "Whisper base (multilingual)", Version: Version, History: []Summary{},
			RuntimeState: "checking", RuntimeMessage: "Checking the local transcription engine",
		},
		install: InstallModels,
		client:  ModelHTTPClient(),
	}
}

func (s *Service) Initialize(ctx context.Context, manifest []byte) error {
	// Load and publish history first so an unhealthy runtime still leaves the
	// user's saved transcripts visible for reading and export. Only a
	// directory-read failure is fatal at this step.
	if err := os.MkdirAll(s.store.Dir, 0700); err != nil {
		return s.failSetup(fmt.Errorf("create transcript storage: %w", err))
	}
	history, damaged, err := s.store.List()
	if err != nil {
		return s.failSetup(err)
	}
	s.mu.Lock()
	s.snapshot.History = history
	if len(damaged) > 0 {
		s.snapshot.HistoryWarning = "Some saved transcripts could not be read and were left in place: " + strings.Join(damaged, ", ")
	}
	s.mu.Unlock()

	if err := os.MkdirAll(filepath.Join(s.dataDir, "work"), 0700); err != nil {
		return s.failSetup(fmt.Errorf("create audio workspace: %w", err))
	}
	if err := cleanInterruptedJobs(filepath.Join(s.dataDir, "work")); err != nil {
		return s.failSetup(err)
	}
	if err := validateCPU(detectCPUFeatures()); err != nil {
		return s.failSetup(err)
	}
	parsed, err := ParseManifest(manifest)
	if err != nil {
		return s.failSetup(err)
	}
	s.manifest = parsed
	s.mu.Lock()
	s.snapshot.RuntimeTotalBytes = ModelInstalledSize(parsed)
	s.mu.Unlock()
	cleanupErr := cleanInterruptedRuntimeInstall(s.dataDir)
	recoveryErr := errors.Join(
		recoverRuntimeBackup(ctx, s.dataDir, parsed),
		recoverModelsBackup(ctx, s.dataDir, parsed),
	)
	if toolsErr := VerifyTools(ctx, s.packageRoot, parsed); toolsErr == nil {
		s.engine.Root = s.packageRoot
		if err := VerifyModels(ctx, s.dataDir, parsed); err == nil {
			cleanupErr = errors.Join(cleanupErr, cleanReplacedModelsBackup(s.dataDir))
			if err := VerifyTools(ctx, s.dataDir, parsed); err == nil {
				cleanupErr = errors.Join(cleanupErr, cleanReplacedRuntimeBackup(s.dataDir))
			}
			s.setReady(s.packageRoot, s.dataDir, cleanupErr)
			return nil
		}
		if err := VerifyModels(ctx, s.packageRoot, parsed); err == nil {
			s.setReady(s.packageRoot, s.packageRoot, cleanupErr)
			return nil
		}
	} else {
		if err := VerifyManifest(ctx, s.dataDir, parsed); err == nil {
			cleanupErr = errors.Join(
				cleanupErr,
				cleanReplacedRuntimeBackup(s.dataDir),
				cleanReplacedModelsBackup(s.dataDir),
			)
			s.setReady(s.dataDir, s.dataDir, cleanupErr)
			return nil
		}
		return s.failSetup(fmt.Errorf(
			"bundled transcription tools are missing or damaged: %w. Extract the complete application download again",
			toolsErr,
		))
	}
	s.mu.Lock()
	s.snapshot.Ready = false
	s.snapshot.RuntimeState = "required"
	s.snapshot.RuntimeMessage = "Download the speech models to begin"
	if runtimeErr := errors.Join(cleanupErr, recoveryErr); runtimeErr != nil {
		s.snapshot.RuntimeError = runtimeErr.Error()
	} else {
		s.snapshot.RuntimeError = ""
	}
	s.mu.Unlock()
	return nil
}

func (s *Service) setReady(toolsRoot, modelRoot string, warning error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.engine.Root = toolsRoot
	s.engine.ModelRoot = modelRoot
	s.snapshot.Ready = true
	s.snapshot.RuntimeState = "ready"
	s.snapshot.RuntimeMessage = "Running offline"
	if warning != nil {
		s.snapshot.RuntimeError = warning.Error()
	} else {
		s.snapshot.RuntimeError = ""
	}
}

func (s *Service) failSetup(err error) error {
	s.mu.Lock()
	s.snapshot.SetupError = err.Error()
	s.snapshot.RuntimeState = "failed"
	s.mu.Unlock()
	return err
}

func (s *Service) InstallModels(ctx context.Context) error {
	s.mu.Lock()
	if s.snapshot.Ready {
		s.mu.Unlock()
		return nil
	}
	if s.installDone != nil {
		s.mu.Unlock()
		return errors.New("the local transcription engine is already downloading")
	}
	if len(s.manifest.Files) == 0 {
		s.mu.Unlock()
		return errors.New("the runtime manifest is unavailable")
	}
	installCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	s.installCancel = cancel
	s.installDone = done
	s.snapshot.RuntimeState = "downloading"
	s.snapshot.RuntimeMessage = "Downloading the speech models"
	s.snapshot.RuntimeError = ""
	s.snapshot.RuntimeProgress = 0
	s.snapshot.RuntimeDownloadedBytes = 0
	s.snapshot.RuntimeDownloadTotalBytes = 0
	manifest := Manifest{Files: append([]Asset(nil), s.manifest.Files...)}
	installer := s.install
	client := s.client
	toolsRoot := s.engine.Root
	s.mu.Unlock()
	defer func() {
		cancel()
		close(done)
		s.mu.Lock()
		s.installCancel = nil
		s.installDone = nil
		s.mu.Unlock()
	}()

	installErr := installer(installCtx, client, s.dataDir, manifest, s.runtimeProgress)
	s.mu.Lock()
	defer s.mu.Unlock()
	var installWarning error
	if installErr != nil {
		cleanupErr, ok := installErr.(*modelActivationCleanupError)
		if ok {
			installWarning = cleanupErr
		} else {
			s.snapshot.Ready = false
			s.snapshot.RuntimeState = "failed"
			s.snapshot.RuntimeMessage = "The speech models were not installed"
			s.snapshot.RuntimeError = installErr.Error()
			return installErr
		}
	}
	if err := VerifyModels(installCtx, s.dataDir, manifest); err != nil {
		s.snapshot.Ready = false
		s.snapshot.RuntimeState = "failed"
		s.snapshot.RuntimeMessage = "The downloaded models could not be verified"
		s.snapshot.RuntimeError = err.Error()
		return err
	}
	if err := VerifyTools(installCtx, toolsRoot, manifest); err != nil {
		s.snapshot.Ready = false
		s.snapshot.RuntimeState = "failed"
		s.snapshot.RuntimeMessage = "The bundled transcription tools could not be verified"
		s.snapshot.RuntimeError = err.Error()
		return err
	}
	s.engine.Root = toolsRoot
	s.engine.ModelRoot = s.dataDir
	s.snapshot.Ready = true
	s.snapshot.RuntimeState = "ready"
	s.snapshot.RuntimeMessage = "Running offline"
	if installWarning != nil {
		s.snapshot.RuntimeError = installWarning.Error()
	} else {
		s.snapshot.RuntimeError = ""
	}
	s.snapshot.RuntimeProgress = 100
	return nil
}

func (s *Service) runtimeProgress(state string, downloaded, total int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshot.RuntimeState = state
	s.snapshot.RuntimeDownloadedBytes = downloaded
	if total > 0 {
		s.snapshot.RuntimeDownloadTotalBytes = total
		s.snapshot.RuntimeProgress = min(100, float64(downloaded)/float64(total)*100)
	}
	if state == "installing" {
		s.snapshot.RuntimeMessage = "Verifying and installing the speech models"
	} else {
		s.snapshot.RuntimeMessage = "Downloading the speech models"
	}
}

func (s *Service) Status() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.snapshot
	out.History = append([]Summary{}, s.snapshot.History...)
	if s.snapshot.Job != nil {
		job := *s.snapshot.Job
		out.Job = &job
	}
	return out
}

func (s *Service) Inspect(ctx context.Context, path string) (FileInfo, error) {
	if !s.Status().Ready {
		return FileInfo{}, errors.New("the local transcription engine is not ready")
	}
	return s.engine.Inspect(ctx, path)
}

func (s *Service) Start(ctx context.Context, path, language string) error {
	if !supported(language, languages) {
		return errors.New("choose a supported speech language or automatic detection")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.snapshot.Ready {
		return errors.New("the local transcription engine is not ready")
	}
	if s.snapshot.Job.active() {
		return errors.New("a transcription is already running")
	}
	idBytes := make([]byte, 16)
	if _, err := rand.Read(idBytes); err != nil {
		return fmt.Errorf("create job ID: %w", err)
	}
	id := hex.EncodeToString(idBytes)
	jobCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.done = make(chan struct{})
	s.snapshot.Job = &Job{ID: id, State: "preparing", Message: "Checking your media file", FileName: filepath.Base(path)}
	go s.run(jobCtx, path, language, id, s.done)
	return nil
}

func (s *Service) run(ctx context.Context, path, language, id string, done chan struct{}) {
	defer close(done)
	file, err := s.engine.Inspect(ctx, path)
	var transcript Transcript
	if err == nil {
		workdir := filepath.Join(s.dataDir, "work", id)
		err = os.Mkdir(workdir, 0700)
		if err == nil {
			transcript, err = s.engine.Transcribe(ctx, file, language, workdir, s.progress)
			// Only the exact per-job directory we created is removed.
			if cleanupErr := os.RemoveAll(workdir); cleanupErr != nil {
				err = errors.Join(err, fmt.Errorf("remove temporary audio: %w", cleanupErr))
			}
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	defer s.cancel()
	if ctx.Err() != nil {
		s.snapshot.Job.State = "cancelled"
		s.snapshot.Job.Message = "Transcription cancelled. Your original file was not changed."
		if err != nil && !errors.Is(err, context.Canceled) {
			s.snapshot.Job.Error = err.Error()
		}
		return
	}
	if err == nil {
		transcript.ID = id
		transcript.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
		err = s.store.Save(transcript)
	}
	if err != nil {
		s.snapshot.Job.State = "failed"
		s.snapshot.Job.Message = "Transcription could not finish"
		s.snapshot.Job.Error = err.Error()
		return
	}
	s.snapshot.History = append([]Summary{transcript.Summary}, s.snapshot.History...)
	s.snapshot.Job.State = "completed"
	s.snapshot.Job.Progress = 100
	s.snapshot.Job.TranscriptID = id
	s.snapshot.Job.Message = "Transcript saved on this computer"
	if len(transcript.Segments) == 0 {
		s.snapshot.Job.Message = "No speech was detected in this file"
	}
}

func (s *Service) progress(state string, progress float64, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshot.Job.State = state
	s.snapshot.Job.Progress = max(s.snapshot.Job.Progress, progress)
	s.snapshot.Job.Message = message
}

func (s *Service) Cancel() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.snapshot.Job.active() {
		return errors.New("there is no running transcription to cancel")
	}
	s.cancel()
	s.snapshot.Job.Message = "Cancelling and removing temporary audio"
	return nil
}

func (s *Service) Close() {
	s.mu.Lock()
	if s.cancel != nil {
		s.cancel()
	}
	done := s.done
	if s.installCancel != nil {
		s.installCancel()
	}
	installDone := s.installDone
	s.mu.Unlock()
	if done != nil {
		<-done
	}
	if installDone != nil {
		<-installDone
	}
}

func (s *Service) Get(id string) (Transcript, error) {
	return s.store.Get(id)
}

func cleanInterruptedJobs(workDir string) error {
	entries, err := os.ReadDir(workDir)
	if err != nil {
		return fmt.Errorf("read temporary audio workspace: %w", err)
	}
	for _, entry := range entries {
		// The desktop's single-instance lock is held before initialization.
		if entry.IsDir() && validID.MatchString(entry.Name()) {
			if err := os.RemoveAll(filepath.Join(workDir, entry.Name())); err != nil {
				return fmt.Errorf("remove audio from interrupted transcription: %w", err)
			}
		}
	}
	return nil
}
