package transcribe

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Store struct {
	Dir string
}

var validID = regexp.MustCompile(`^[a-f0-9]{32}$`)

// One saved transcript is bounded to keep runaway input from consuming memory;
// six hours at Whisper base is well under this.
const maxTranscriptBytes = 32 * 1024 * 1024

func (s Store) Get(id string) (Transcript, error) {
	var t Transcript
	if !validID.MatchString(id) {
		return t, errors.New("invalid transcript ID")
	}
	f, err := os.Open(filepath.Join(s.Dir, id+".json"))
	if err != nil {
		return t, fmt.Errorf("open saved transcript: %w", err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxTranscriptBytes+1))
	if err != nil {
		return t, fmt.Errorf("read saved transcript: %w", err)
	}
	if len(data) > maxTranscriptBytes {
		return t, errors.New("saved transcript is larger than the supported limit")
	}
	// Unmarshal rejects trailing bytes, so a valid JSON prefix cannot silently pass.
	if err := json.Unmarshal(data, &t); err != nil {
		return t, fmt.Errorf("read saved transcript: %w", err)
	}
	if err := validateTranscript(t, id); err != nil {
		return t, err
	}
	return t, nil
}

// validateTranscript enforces the persisted invariants the app depends on.
// Detected language is preserved as-is: Whisper detects languages outside the
// manual-language menu, and rejecting them would erase valid history.
func validateTranscript(t Transcript, id string) error {
	if t.ID != id {
		return errors.New("saved transcript ID does not match its filename")
	}
	if t.FileName == "" {
		return errors.New("saved transcript is missing its source filename")
	}
	if t.Language == "" {
		return errors.New("saved transcript is missing its detected language")
	}
	if t.CreatedAt == "" {
		return errors.New("saved transcript is missing a creation date")
	}
	if _, err := time.Parse(time.RFC3339Nano, t.CreatedAt); err != nil {
		return fmt.Errorf("saved transcript has an invalid creation date: %w", err)
	}
	if t.DurationMs < 0 {
		return errors.New("saved transcript has a negative duration")
	}
	if t.WordCount < 0 {
		return errors.New("saved transcript has a negative word count")
	}
	if t.Segments == nil {
		return errors.New("saved transcript is missing segments")
	}
	var prevStart int64
	texts := make([]string, 0, len(t.Segments))
	for i, seg := range t.Segments {
		if seg.StartMs < 0 || seg.EndMs <= seg.StartMs || seg.EndMs > t.DurationMs {
			return fmt.Errorf("saved transcript segment %d has invalid timing", i)
		}
		if i > 0 && seg.StartMs < prevStart {
			return fmt.Errorf("saved transcript segment %d starts before segment %d", i, i-1)
		}
		if strings.TrimSpace(seg.Text) == "" {
			return fmt.Errorf("saved transcript segment %d has no text", i)
		}
		prevStart = seg.StartMs
		texts = append(texts, seg.Text)
	}
	// The engine writes Text as segment texts joined by newlines (empty for a
	// silent transcript). Rejecting any other value guarantees the UI cannot
	// display one set of words and then copy or export a different set.
	if t.Text != strings.Join(texts, "\n") {
		return errors.New("saved transcript text does not match its segment texts")
	}
	if len(strings.Fields(t.Text)) != t.WordCount {
		return errors.New("saved transcript word count does not match its text")
	}
	return nil
}

// List returns healthy summaries plus filenames that could not be read.
// Damaged files are preserved in place so the user can recover them by hand.
// Only a directory-read failure aborts loading.
func (s Store) List() ([]Summary, []string, error) {
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		return nil, nil, fmt.Errorf("read transcript history: %w", err)
	}
	out := []Summary{}
	var damaged []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || filepath.Ext(name) != ".json" {
			continue
		}
		id := strings.TrimSuffix(name, ".json")
		if !validID.MatchString(id) {
			// Files with unrecognized names were not written by the app; skip silently.
			continue
		}
		t, err := s.Get(id)
		if err != nil {
			damaged = append(damaged, name)
			continue
		}
		out = append(out, t.Summary)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt > out[j].CreatedAt })
	sort.Strings(damaged)
	return out, damaged, nil
}

func (s Store) Save(t Transcript) error {
	if !validID.MatchString(t.ID) {
		return errors.New("invalid transcript ID")
	}
	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	return WriteAtomic(filepath.Join(s.Dir, t.ID+".json"), data)
}

func WriteAtomic(path string, data []byte) (err error) {
	f, err := os.CreateTemp(filepath.Dir(path), ".transcribeme-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary output: %w", err)
	}
	name := f.Name()
	defer func() {
		if cleanupErr := os.Remove(name); cleanupErr != nil && !os.IsNotExist(cleanupErr) {
			err = errors.Join(err, fmt.Errorf("remove temporary output: %w", cleanupErr))
		}
	}()
	if _, err := f.Write(data); err != nil {
		return errors.Join(err, f.Close())
	}
	if err := f.Sync(); err != nil {
		return errors.Join(err, f.Close())
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return fmt.Errorf("save output: %w", err)
	}
	return nil
}

func Timestamp(ms int64, separator string) string {
	return fmt.Sprintf("%02d:%02d:%02d%s%03d", ms/3600000, (ms/60000)%60, (ms/1000)%60, separator, ms%1000)
}

func Format(t Transcript, format string) ([]byte, error) {
	if format == "txt" {
		if t.Text == "" {
			return []byte{}, nil
		}
		return []byte(strings.ReplaceAll(t.Text, "\n", "\r\n") + "\r\n"), nil
	}
	if format != "srt" && format != "vtt" {
		return nil, errors.New("choose TXT, SRT, or VTT")
	}
	var b strings.Builder
	separator := ","
	if format == "vtt" {
		b.WriteString("WEBVTT\n\n")
		separator = "."
	}
	for i, s := range t.Segments {
		if s.StartMs < 0 || s.EndMs <= s.StartMs {
			return nil, errors.New("cannot export invalid segment timestamps")
		}
		if format == "srt" {
			b.WriteString(strconv.Itoa(i+1) + "\n")
		}
		b.WriteString(Timestamp(s.StartMs, separator) + " --> " + Timestamp(s.EndMs, separator) + "\n")
		text := strings.Join(strings.Fields(s.Text), " ")
		text = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(text)
		b.WriteString(text + "\n\n")
	}
	return []byte(b.String()), nil
}
