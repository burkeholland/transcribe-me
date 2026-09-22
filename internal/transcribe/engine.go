package transcribe

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const mediaExtensions = ".mp4 .m4v .mov .mkv .webm .avi .wmv .mpeg .mpg .ts .mp3 .wav .m4a .aac .flac .ogg .opus .wma"
const languages = "auto en es fr de it pt ja zh ko hi ar ru nl pl uk tr vi id sv da no fi el he cs ro hu th"
const formats = "mov,matroska,webm,avi,asf,mpeg,mpegts,mp3,wav,flac,ogg,aac"

func supported(value, list string) bool {
	for _, item := range strings.Fields(list) {
		if value == item {
			return true
		}
	}
	return false
}

func (e Engine) Inspect(ctx context.Context, path string) (FileInfo, error) {
	var info FileInfo
	if !filepath.IsAbs(path) {
		return info, errors.New("choose an absolute path to a local video or audio file")
	}
	path = filepath.Clean(path)
	if !supported(strings.ToLower(filepath.Ext(path)), mediaExtensions) {
		return info, errors.New("unsupported file type; choose MP4, MOV, MKV, WebM, AVI, or a supported audio file")
	}
	st, err := os.Stat(path)
	if err != nil {
		return info, fmt.Errorf("open media file: %w", err)
	}
	if !st.Mode().IsRegular() || st.Size() == 0 {
		return info, errors.New("choose a non-empty video or audio file, not a folder")
	}
	probeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	data, err := e.Run(probeCtx, e.tool("ffprobe"), []string{
		"-v", "error", "-protocol_whitelist", "file,pipe", "-format_whitelist", formats,
		"-show_entries", "format=duration:stream=codec_type,height", "-of", "json", "-i", path,
	}, nil)
	if err != nil {
		return info, fmt.Errorf("cannot read this media file: %w", err)
	}
	var probe struct {
		Streams []struct {
			CodecType string `json:"codec_type"`
			Height    int    `json:"height"`
		} `json:"streams"`
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return info, fmt.Errorf("invalid media metadata: %w", err)
	}
	hasAudio := false
	height := 0
	for _, stream := range probe.Streams {
		hasAudio = hasAudio || stream.CodecType == "audio"
		if stream.CodecType == "video" && stream.Height > height {
			height = stream.Height
		}
	}
	if !hasAudio {
		return info, errors.New("this file has no audio track; choose a video that contains speech")
	}
	duration, err := strconv.ParseFloat(probe.Format.Duration, 64)
	if err != nil || math.IsNaN(duration) || math.IsInf(duration, 0) || duration <= 0 {
		return info, errors.New("the file has no readable duration; re-export it as MP4 or WAV and try again")
	}
	// The WAV and Whisper decoder hold uncompressed audio. Bound resource use explicitly.
	if duration > 6*60*60 {
		return info, errors.New("files longer than 6 hours are not supported; split the recording into shorter files")
	}
	return FileInfo{Path: path, Name: filepath.Base(path), Size: st.Size(), DurationMs: int64(math.Round(duration * 1000)), Height: height}, nil
}

var whisperProgress = regexp.MustCompile(`progress\s*=\s*(\d+)%`)

func (e Engine) Transcribe(ctx context.Context, file FileInfo, language, workdir string, progress func(string, float64, string)) (Transcript, error) {
	var result Transcript
	wav := filepath.Join(workdir, "audio.wav")
	output := filepath.Join(workdir, "transcript")
	progress("preparing", 1, "Extracting the audio on your computer")
	_, err := e.Run(ctx, e.tool("ffmpeg"), []string{
		"-hide_banner", "-nostdin", "-v", "error", "-xerror",
		"-protocol_whitelist", "file,pipe", "-format_whitelist", formats,
		"-i", file.Path, "-map", "0:a:0", "-vn", "-sn", "-dn",
		"-ac", "1", "-ar", "16000", "-c:a", "pcm_s16le",
		"-progress", "pipe:1", "-nostats", "-y", wav,
	}, func(line string) {
		if value, ok := strings.CutPrefix(line, "out_time_us="); ok {
			if us, err := strconv.ParseFloat(value, 64); err == nil {
				progress("preparing", min(15, max(1, us/float64(file.DurationMs)/1000*15)), "Extracting the audio on your computer")
			}
		}
	})
	if err != nil {
		return result, fmt.Errorf("audio extraction failed: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	progress("transcribing", 15, "Listening with Whisper base. Your file stays on this computer.")
	threads := max(1, min(8, runtime.NumCPU()-1))
	_, err = e.Run(ctx, e.tool("whisper"), []string{
		"-m", e.tool("model"), "-f", wav, "-l", language,
		"-t", strconv.Itoa(threads), "-ng", "-pp", "-oj", "-of", output,
		"--vad", "-vm", e.tool("vad"), "-ml", "84", "-sow",
	}, func(line string) {
		m := whisperProgress.FindStringSubmatch(line)
		if len(m) == 2 {
			pct, _ := strconv.Atoi(m[1])
			progress("transcribing", min(95, 15+float64(pct)*0.8), "Turning speech into text")
		}
	})
	if err != nil {
		return result, fmt.Errorf("transcription failed: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	f, err := os.Open(output + ".json")
	if err != nil {
		return result, fmt.Errorf("Whisper did not produce a transcript: %w", err)
	}
	defer f.Close()
	var raw struct {
		Result struct {
			Language string `json:"language"`
		} `json:"result"`
		Transcription *[]struct {
			Offsets struct {
				From int64 `json:"from"`
				To   int64 `json:"to"`
			} `json:"offsets"`
			Text string `json:"text"`
		} `json:"transcription"`
	}
	if err := json.NewDecoder(f).Decode(&raw); err != nil {
		return result, fmt.Errorf("read Whisper output: %w", err)
	}
	if raw.Transcription == nil || raw.Result.Language == "" {
		return result, errors.New("Whisper returned an incomplete transcript")
	}
	result.Segments = []Segment{}
	parts := []string{}
	var last int64
	for _, s := range *raw.Transcription {
		text := strings.Join(strings.Fields(s.Text), " ")
		if text == "" {
			continue
		}
		if s.Offsets.From < 0 || s.Offsets.To < s.Offsets.From || s.Offsets.From < last {
			return result, errors.New("Whisper returned invalid segment timestamps")
		}
		start, end := min(s.Offsets.From, file.DurationMs), min(s.Offsets.To, file.DurationMs)
		if end <= start {
			continue
		}
		result.Segments = append(result.Segments, Segment{StartMs: start, EndMs: end, Text: text})
		parts = append(parts, text)
		last = s.Offsets.From
	}
	result.Text = strings.Join(parts, "\n")
	result.FileName = file.Name
	result.SourcePath = file.Path
	result.Size = file.Size
	result.Height = file.Height
	result.DurationMs = file.DurationMs
	result.Language = raw.Result.Language
	result.WordCount = len(strings.Fields(result.Text))
	return result, nil
}
