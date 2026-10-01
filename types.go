package typetube

import (
	"fmt"
)

type TypeTubeError struct {
	Message    string
	StatusCode int
}

func (e *TypeTubeError) Error() string {
	if e.StatusCode > 0 {
		return fmt.Sprintf("typetube error (%d): %s", e.StatusCode, e.Message)
	}
	return fmt.Sprintf("typetube error: %s", e.Message)
}

type Thumbnail struct {
	URL    string `json:"url"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
}

type AudioStream struct {
	ITag     int    `json:"itag"`
	Quality  string `json:"quality"`
	MimeType string `json:"mimeType"`
	Bitrate  int64  `json:"bitrate"`
	URL      string `json:"url"`
	IsHiFi   bool   `json:"isHiFi,omitempty"`
	RawURL   string `json:"rawUrl,omitempty"`
}

type VideoStream struct {
	ITag       int    `json:"itag"`
	Quality    string `json:"quality"`
	Resolution string `json:"resolution,omitempty"`
	MimeType   string `json:"mimeType"`
	URL        string `json:"url"`
}

type TrackResult struct {
	Success         bool          `json:"success"`
	Query           string        `json:"query"`
	ID              string        `json:"id"`
	Title           string        `json:"title"`
	Author          string        `json:"author"`
	Uploader        string        `json:"uploader,omitempty"`
	ArtistAvatar    string        `json:"artistAvatar,omitempty"`
	DurationSeconds int           `json:"durationSeconds"`
	Thumbnail       string        `json:"thumbnail"`
	LatencyMs       float64       `json:"latencyMs"`
	BestAudio       *AudioStream  `json:"bestAudio,omitempty"`
	BestVideo       *VideoStream  `json:"bestVideo,omitempty"`
	Thumbnails      []Thumbnail   `json:"thumbnails,omitempty"`
	AudioStreams    []AudioStream `json:"audioStreams,omitempty"`
	VideoStreams    []VideoStream `json:"videoStreams,omitempty"`
}

type SearchItem struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	URL      string `json:"url"`
	Duration int    `json:"durationSeconds,omitempty"`
	Uploader string `json:"uploader,omitempty"`
}

type UsageInfo struct {
	Success             bool   `json:"success"`
	KeyType             string `json:"keyType"`
	Limit               int    `json:"limit"`
	Used                int    `json:"used"`
	Remaining           int    `json:"remaining"`
	ResetInSeconds      int    `json:"resetInSeconds"`
	WindowSeconds       int    `json:"windowSeconds"`
	DailyLimit          int    `json:"dailyLimit"`
	DailyUsed           int    `json:"dailyUsed"`
	DailyRemaining      int    `json:"dailyRemaining"`
	DailyResetInSeconds int    `json:"dailyResetInSeconds"`
}

type DownloadProgress struct {
	DownloadedBytes int64
	TotalBytes      int64
	Percent         float64
	SpeedMbps       float64
}

type ProgressFunc func(progress DownloadProgress)

type DownloadResult struct {
	Success         bool
	FilePath        string
	FileName        string
	SizeBytes       int64
	FileSizeMB      float64
	DurationSec     float64
	SpeedMbps       float64
	ID              string
	Title           string
	Author          string
	DurationSeconds int
	Track           *TrackResult
}

type DownloadConfig struct {
	Workers          int
	ChunkSizeBytes   int64
	Quality          string
	ProgressCallback ProgressFunc
}
