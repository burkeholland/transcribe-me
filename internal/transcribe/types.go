package transcribe

const Version = "0.2.0"

type FileInfo struct {
	Path       string `json:"path"`
	Name       string `json:"name"`
	Size       int64  `json:"size"`
	DurationMs int64  `json:"durationMs"`
	Height     int    `json:"height"`
}

type Summary struct {
	ID         string `json:"id"`
	FileName   string `json:"fileName"`
	CreatedAt  string `json:"createdAt"`
	Language   string `json:"language"`
	DurationMs int64  `json:"durationMs"`
	WordCount  int    `json:"wordCount"`
	SourcePath string `json:"sourcePath"`
	Size       int64  `json:"size"`
	Height     int    `json:"height"`
}

type Segment struct {
	StartMs int64  `json:"startMs"`
	EndMs   int64  `json:"endMs"`
	Text    string `json:"text"`
}

type Transcript struct {
	Summary
	Text     string    `json:"text"`
	Segments []Segment `json:"segments"`
}

type Job struct {
	ID           string  `json:"id"`
	State        string  `json:"state"`
	Progress     float64 `json:"progress"`
	Message      string  `json:"message"`
	FileName     string  `json:"fileName"`
	Error        string  `json:"error"`
	TranscriptID string  `json:"transcriptID"`
}

type Snapshot struct {
	Ready                     bool      `json:"ready"`
	SetupError                string    `json:"setupError"`
	HistoryWarning            string    `json:"historyWarning"`
	RuntimeState              string    `json:"runtimeState"`
	RuntimeMessage            string    `json:"runtimeMessage"`
	RuntimeError              string    `json:"runtimeError"`
	RuntimeProgress           float64   `json:"runtimeProgress"`
	RuntimeDownloadedBytes    int64     `json:"runtimeDownloadedBytes"`
	RuntimeDownloadTotalBytes int64     `json:"runtimeDownloadTotalBytes"`
	RuntimeTotalBytes         int64     `json:"runtimeTotalBytes"`
	ModelName                 string    `json:"modelName"`
	Version                   string    `json:"version"`
	Job                       *Job      `json:"job"`
	History                   []Summary `json:"history"`
}

func (j *Job) active() bool {
	return j != nil && (j.State == "preparing" || j.State == "transcribing")
}
