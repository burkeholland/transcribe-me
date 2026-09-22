package transcribe

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func exampleTranscript() Transcript {
	return Transcript{
		Summary:  Summary{ID: strings.Repeat("a", 32), FileName: "Example.mp4", CreatedAt: "2026-09-21T19:00:00Z", Language: "en", DurationMs: 3723123, WordCount: 4},
		Text:     "Hello <world> & friends.",
		Segments: []Segment{{StartMs: 1250, EndMs: 3723123, Text: "Hello <world> & friends."}},
	}
}

func TestStoreRoundTrip(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	input := exampleTranscript()
	if err := s.Save(input); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(input.ID)
	if err != nil || got.Text != input.Text || got.Segments[0] != input.Segments[0] {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	list, damaged, err := s.List()
	if err != nil || len(list) != 1 || len(damaged) != 0 || list[0].ID != input.ID {
		t.Fatalf("list=%+v damaged=%+v err=%v", list, damaged, err)
	}
	if _, err := s.Get("..\\outside"); err == nil {
		t.Fatal("accepted unsafe ID")
	}
	damagedName := strings.Repeat("b", 32) + ".json"
	if err := os.WriteFile(filepath.Join(s.Dir, damagedName), []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	list, damaged, err = s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != input.ID {
		t.Fatalf("healthy history lost when a damaged file exists: %+v", list)
	}
	if len(damaged) != 1 || damaged[0] != damagedName {
		t.Fatalf("damaged file not reported: %+v", damaged)
	}
	if _, err := os.Stat(filepath.Join(s.Dir, damagedName)); err != nil {
		t.Fatal("damaged history file must be preserved for user recovery")
	}
	// Files whose name is not a valid transcript ID are not ours; they should
	// neither be listed nor be reported as damaged.
	stray := filepath.Join(s.Dir, "notes.json")
	if err := os.WriteFile(stray, []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	_, damaged, err = s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(damaged) != 1 || damaged[0] != damagedName {
		t.Fatalf("unrelated json file was picked up: %+v", damaged)
	}
}

func TestValidateTranscriptRejects(t *testing.T) {
	id := strings.Repeat("a", 32)
	valid := Transcript{
		Summary: Summary{ID: id, FileName: "ok.mp4", CreatedAt: "2026-09-21T19:00:00Z", Language: "sv", DurationMs: 5000, WordCount: 3},
		Text:    "one two\nthree",
		Segments: []Segment{
			{StartMs: 0, EndMs: 2000, Text: "one two"},
			{StartMs: 2000, EndMs: 5000, Text: "three"},
		},
	}
	if err := validateTranscript(valid, id); err != nil {
		t.Fatalf("healthy transcript rejected: %v", err)
	}
	silent := valid
	silent.Text = ""
	silent.WordCount = 0
	silent.Segments = []Segment{}
	if err := validateTranscript(silent, id); err != nil {
		t.Fatalf("silent transcript rejected: %v", err)
	}
	// A language outside the manual menu must still be accepted; it is what
	// Whisper detected and rejecting it would erase valid history.
	detectedOnly := valid
	detectedOnly.Language = "cy"
	if err := validateTranscript(detectedOnly, id); err != nil {
		t.Fatalf("detected-only language rejected: %v", err)
	}
	// ID mismatch.
	if err := validateTranscript(valid, strings.Repeat("b", 32)); err == nil {
		t.Fatal("mismatched id accepted")
	}
	base := valid
	cases := []struct {
		name   string
		mutate func(*Transcript)
	}{
		{"empty filename", func(v *Transcript) { v.FileName = "" }},
		{"empty language", func(v *Transcript) { v.Language = "" }},
		{"empty date", func(v *Transcript) { v.CreatedAt = "" }},
		{"bad date", func(v *Transcript) { v.CreatedAt = "yesterday" }},
		{"negative duration", func(v *Transcript) { v.DurationMs = -1 }},
		{"negative words", func(v *Transcript) { v.WordCount = -1 }},
		{"nil segments", func(v *Transcript) { v.Segments = nil }},
		{"segment starts negative", func(v *Transcript) {
			v.Segments = []Segment{{StartMs: -1, EndMs: 1000, Text: "x"}}
			v.Text = "x"
			v.WordCount = 1
		}},
		{"zero-length segment", func(v *Transcript) {
			v.Segments = []Segment{{StartMs: 500, EndMs: 500, Text: "x"}}
			v.Text = "x"
			v.WordCount = 1
		}},
		{"segment exceeds duration", func(v *Transcript) {
			v.Segments = []Segment{{StartMs: 0, EndMs: v.DurationMs + 1, Text: "x"}}
			v.Text = "x"
			v.WordCount = 1
		}},
		{"segments out of order", func(v *Transcript) {
			v.Segments = []Segment{{StartMs: 2000, EndMs: 3000, Text: "a"}, {StartMs: 1000, EndMs: 4000, Text: "b"}}
			v.Text = "a\nb"
			v.WordCount = 2
		}},
		{"blank segment text", func(v *Transcript) {
			v.Segments = []Segment{{StartMs: 0, EndMs: 1000, Text: "   "}}
			v.Text = ""
			v.WordCount = 0
		}},
		{"text disagrees with segments", func(v *Transcript) {
			v.Text = "totally different"
			v.WordCount = 2
		}},
		{"silent text with populated string", func(v *Transcript) {
			v.Segments = []Segment{}
			v.Text = "leftover"
			v.WordCount = 1
		}},
		{"word count mismatch", func(v *Transcript) { v.WordCount = 99 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bad := base
			bad.Segments = append([]Segment(nil), base.Segments...)
			tc.mutate(&bad)
			if err := validateTranscript(bad, id); err == nil {
				t.Fatalf("accepted invalid transcript: %+v", bad)
			}
		})
	}
}

// A persisted record with a missing or blank source filename must never
// surface in the history view. Otherwise the sidebar shows a nameless entry
// the user cannot meaningfully identify or click.
func TestBlankFileNameCannotProduceHistoryEntry(t *testing.T) {
	dir := t.TempDir()
	s := Store{Dir: dir}
	blank := exampleTranscript()
	blank.FileName = ""
	// Save skips validation, so we can write the invalid record directly.
	if err := s.Save(blank); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(blank.ID); err == nil {
		t.Fatal("Get accepted a record with a blank filename")
	}
	list, damaged, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("blank-filename record was surfaced in history: %+v", list)
	}
	if len(damaged) != 1 {
		t.Fatalf("blank-filename record was not reported as damaged: %+v", damaged)
	}
}

// The engine derives Text from segments, so a persisted record whose Text
// disagrees with the sum of segment texts must not become viewable content.
// Otherwise the reader panel and the copy/export path would show different words.
func TestTextAndSegmentsMustAgree(t *testing.T) {
	dir := t.TempDir()
	s := Store{Dir: dir}
	mismatch := exampleTranscript()
	mismatch.Text = "totally different words"
	if err := s.Save(mismatch); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(mismatch.ID); err == nil {
		t.Fatal("Get accepted a record whose text disagrees with its segments")
	}
}

func TestGetRejectsTrailingJSON(t *testing.T) {
	id := strings.Repeat("a", 32)
	dir := t.TempDir()
	good := `{"id":"` + id + `","fileName":"ok.mp4","createdAt":"2026-09-21T19:00:00Z","language":"en","durationMs":1000,"wordCount":1,"text":"hi","segments":[{"startMs":0,"endMs":1000,"text":"hi"}]}`
	if err := os.WriteFile(filepath.Join(dir, id+".json"), []byte(good+"garbage"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := (Store{Dir: dir}).Get(id); err == nil {
		t.Fatal("trailing bytes after a valid JSON document were accepted")
	}
}

func TestGetRejectsOversizeFile(t *testing.T) {
	id := strings.Repeat("a", 32)
	dir := t.TempDir()
	// One byte over the limit. The exact contents do not matter because the
	// size guard fires before JSON parsing.
	if err := os.WriteFile(filepath.Join(dir, id+".json"), make([]byte, maxTranscriptBytes+1), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := (Store{Dir: dir}).Get(id); err == nil {
		t.Fatal("oversized transcript accepted")
	}
}

func TestExportFormats(t *testing.T) {
	input := exampleTranscript()
	for _, tc := range []struct{ format, expected string }{
		{"txt", "Hello <world> & friends.\r\n"},
		{"srt", "1\n00:00:01,250 --> 01:02:03,123\nHello &lt;world&gt; &amp; friends.\n\n"},
		{"vtt", "WEBVTT\n\n00:00:01.250 --> 01:02:03.123\nHello &lt;world&gt; &amp; friends.\n\n"},
	} {
		got, err := Format(input, tc.format)
		if err != nil || string(got) != tc.expected {
			t.Errorf("%s: got %q, err %v", tc.format, got, err)
		}
	}
	if _, err := Format(input, "exe"); err == nil {
		t.Fatal("accepted unknown export")
	}
	input.Segments[0].StartMs = -1
	if _, err := Format(input, "srt"); err == nil {
		t.Fatal("accepted invalid times")
	}
}

func TestEmptyTranscript(t *testing.T) {
	for _, format := range []string{"txt", "srt", "vtt"} {
		data, err := Format(Transcript{Segments: []Segment{}}, format)
		if err != nil {
			t.Fatal(err)
		}
		if format == "vtt" && string(data) != "WEBVTT\n\n" {
			t.Fatal("VTT header missing")
		}
	}
}

func TestAtomicWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.txt")
	for _, text := range []string{"first", "replacement"} {
		if err := WriteAtomic(path, []byte(text)); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "replacement" {
		t.Fatalf("data=%q err=%v", data, err)
	}
	dir := t.TempDir()
	if err := WriteAtomic(dir, []byte("cannot replace a directory")); err == nil {
		t.Fatal("invalid target accepted")
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 1 {
		t.Fatalf("temporary file leak: %+v %v", entries, err)
	}
}
