package typetube

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	DefaultHost    = "https://typetube.xysushi.in"
	DefaultTimeout = 15 * time.Second
)

type Option func(*Client)

func WithAPIKey(key string) Option {
	return func(c *Client) {
		c.apiKey = key
	}
}

func WithHost(host string) Option {
	return func(c *Client) {
		c.host = strings.TrimRight(host, "/")
	}
}

func WithTimeout(timeout time.Duration) Option {
	return func(c *Client) {
		c.timeout = timeout
		if c.httpClient != nil {
			c.httpClient.Timeout = timeout
		}
	}
}

func WithFormat(format string) Option {
	return func(c *Client) {
		c.format = format
	}
}

func WithHTTPClient(client *http.Client) Option {
	return func(c *Client) {
		c.httpClient = client
	}
}

type Client struct {
	apiKey     string
	host       string
	timeout    time.Duration
	format     string
	httpClient *http.Client
}

func NewClient(opts ...Option) *Client {
	c := &Client{
		apiKey:  "public",
		host:    DefaultHost,
		timeout: DefaultTimeout,
		format:  "protobuf",
		httpClient: &http.Client{
			Timeout: DefaultTimeout,
		},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

func (c *Client) Usage(ctx context.Context) (*UsageInfo, error) {
	reqURL := fmt.Sprintf("%s/v1/usage", c.host)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-API-Key", c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		var errData map[string]interface{}
		_ = json.Unmarshal(body, &errData)
		errMsg := "failed to fetch usage"
		if msg, ok := errData["error"].(string); ok {
			errMsg = msg
		}
		return nil, &TypeTubeError{Message: errMsg, StatusCode: resp.StatusCode}
	}

	var usage UsageInfo
	if err := json.Unmarshal(body, &usage); err != nil {
		return nil, err
	}
	return &usage, nil
}

func (c *Client) Resolve(ctx context.Context, query string) (*TrackResult, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, errors.New("query cannot be empty")
	}

	params := url.Values{}
	params.Set("q", q)
	params.Set("format", c.format)

	reqURL := fmt.Sprintf("%s/v1/resolve?%s", c.host, params.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}

	if c.format == "protobuf" {
		req.Header.Set("Accept", "application/x-protobuf, application/octet-stream;q=0.9, */*;q=0.8")
	} else {
		req.Header.Set("Accept", "application/json")
	}
	req.Header.Set("X-API-Key", c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	rawBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		var errData map[string]interface{}
		_ = json.Unmarshal(rawBytes, &errData)
		errMsg := "resolution failed"
		if msg, ok := errData["error"].(string); ok {
			errMsg = msg
		}
		if resp.StatusCode == http.StatusUnauthorized && (c.apiKey == "public" || strings.Contains(strings.ToLower(errMsg), "public")) {
			errMsg = "No public API key is available."
		}
		return nil, &TypeTubeError{Message: errMsg, StatusCode: resp.StatusCode}
	}

	contentType := resp.Header.Get("Content-Type")
	if strings.Contains(contentType, "application/x-protobuf") || strings.Contains(contentType, "application/octet-stream") || c.format == "protobuf" {
		return decodeTrackResult(rawBytes)
	}

	var result TrackResult
	if err := json.Unmarshal(rawBytes, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) Search(ctx context.Context, query string, limit int) ([]SearchItem, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, errors.New("search query cannot be empty")
	}
	if limit <= 0 {
		limit = 10
	}

	params := url.Values{}
	params.Set("q", q)
	params.Set("limit", strconv.Itoa(limit))
	params.Set("format", c.format)

	reqURL := fmt.Sprintf("%s/v1/search?%s", c.host, params.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}

	if c.format == "protobuf" {
		req.Header.Set("Accept", "application/x-protobuf, application/octet-stream;q=0.9, */*;q=0.8")
	} else {
		req.Header.Set("Accept", "application/json")
	}
	req.Header.Set("X-API-Key", c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	rawBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		var errData map[string]interface{}
		_ = json.Unmarshal(rawBytes, &errData)
		errMsg := "search failed"
		if msg, ok := errData["error"].(string); ok {
			errMsg = msg
		}
		return nil, &TypeTubeError{Message: errMsg, StatusCode: resp.StatusCode}
	}

	contentType := resp.Header.Get("Content-Type")
	if strings.Contains(contentType, "application/x-protobuf") || strings.Contains(contentType, "application/octet-stream") || c.format == "protobuf" {
		return decodeSearchResults(rawBytes)
	}

	var searchResp struct {
		Results []struct {
			ID              string `json:"id"`
			Title           string `json:"title"`
			URL             string `json:"url"`
			DurationSeconds int    `json:"durationSeconds"`
			Author          string `json:"author"`
			Uploader        string `json:"uploader"`
		} `json:"results"`
	}

	if err := json.Unmarshal(rawBytes, &searchResp); err != nil {
		return nil, err
	}

	items := make([]SearchItem, len(searchResp.Results))
	for i, r := range searchResp.Results {
		uploader := r.Author
		if uploader == "" {
			uploader = r.Uploader
		}
		items[i] = SearchItem{
			ID:       r.ID,
			Title:    r.Title,
			URL:      r.URL,
			Duration: r.DurationSeconds,
			Uploader: uploader,
		}
	}
	return items, nil
}

func (c *Client) DownloadAudio(ctx context.Context, target interface{}, dest string, cfg ...DownloadConfig) (*DownloadResult, error) {
	var config DownloadConfig
	if len(cfg) > 0 {
		config = cfg[0]
	}
	if config.Workers <= 0 {
		config.Workers = 4
	}
	if config.ChunkSizeBytes <= 0 {
		config.ChunkSizeBytes = 1024 * 1024
	}
	if config.Quality == "" {
		config.Quality = "highest"
	}

	var track *TrackResult
	switch v := target.(type) {
	case *TrackResult:
		track = v
	case TrackResult:
		track = &v
	case string:
		res, err := c.Resolve(ctx, v)
		if err != nil {
			return nil, err
		}
		track = res
	default:
		return nil, errors.New("target must be string query/id or TrackResult")
	}

	stream := c.selectAudioStream(track, config.Quality)
	if stream == nil || stream.URL == "" {
		return nil, errors.New("no playable audio stream available")
	}

	finalDest := dest
	if finalDest == "" {
		finalDest = "./"
	}

	stat, err := os.Stat(finalDest)
	isDir := (err == nil && stat.IsDir()) || filepath.Ext(finalDest) == ""

	if isDir {
		if err := os.MkdirAll(finalDest, 0755); err != nil {
			return nil, err
		}
		reg := regexp.MustCompile(`[^a-zA-Z0-9 ._-]`)
		baseName := fmt.Sprintf("%s - %s", track.Author, track.Title)
		safeName := strings.TrimSpace(reg.ReplaceAllString(baseName, ""))
		ext := "opus"
		if strings.Contains(stream.MimeType, "mp4") {
			ext = "m4a"
		}
		finalDest = filepath.Join(finalDest, fmt.Sprintf("%s.%s", safeName, ext))
	} else {
		parentDir := filepath.Dir(finalDest)
		if parentDir != "" {
			if err := os.MkdirAll(parentDir, 0755); err != nil {
				return nil, err
			}
		}
	}

	tStart := time.Now()
	bytesWritten, err := c.downloadRangeParallel(ctx, stream.URL, finalDest, config.Workers, config.ChunkSizeBytes, config.ProgressCallback)
	if err != nil {
		return nil, err
	}

	durSec := time.Since(tStart).Seconds()
	speedMbps := 0.0
	if durSec > 0 {
		speedMbps = (float64(bytesWritten) / (1024.0 * 1024.0)) / durSec
	}
	fileSizeMB := float64(bytesWritten) / (1024.0 * 1024.0)

	return &DownloadResult{
		Success:         true,
		FilePath:        finalDest,
		FileName:        filepath.Base(finalDest),
		SizeBytes:       bytesWritten,
		FileSizeMB:      math.Round(fileSizeMB*100) / 100,
		DurationSec:     math.Round(durSec*100) / 100,
		SpeedMbps:       math.Round(speedMbps*100) / 100,
		ID:              track.ID,
		Title:           track.Title,
		Author:          track.Author,
		DurationSeconds: track.DurationSeconds,
		Track:           track,
	}, nil
}

func (c *Client) selectAudioStream(track *TrackResult, quality string) *AudioStream {
	if len(track.AudioStreams) == 0 {
		return track.BestAudio
	}

	switch strings.ToLower(quality) {
	case "hifi", "256kbps", "highest":
		for i := range track.AudioStreams {
			if track.AudioStreams[i].ITag == 141 || track.AudioStreams[i].ITag == 774 {
				return &track.AudioStreams[i]
			}
		}
	case "128kbps":
		for i := range track.AudioStreams {
			if track.AudioStreams[i].ITag == 140 || track.AudioStreams[i].ITag == 251 {
				return &track.AudioStreams[i]
			}
		}
	}

	if track.BestAudio != nil {
		return track.BestAudio
	}
	return &track.AudioStreams[0]
}

type rangeChunk struct {
	start int64
	end   int64
}

func (c *Client) downloadRangeParallel(
	ctx context.Context,
	urlStr string,
	destPath string,
	workers int,
	chunkSize int64,
	progressFn ProgressFunc,
) (int64, error) {
	probeReq, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		return 0, err
	}
	probeReq.Header.Set("Range", "bytes=0-0")
	probeReq.Header.Set("User-Agent", "Mozilla/5.0")

	probeResp, err := http.DefaultClient.Do(probeReq)
	var totalBytes int64 = 0
	if err == nil {
		defer probeResp.Body.Close()
		cr := probeResp.Header.Get("Content-Range")
		if strings.Contains(cr, "/") {
			parts := strings.Split(cr, "/")
			if len(parts) == 2 {
				val, parseErr := strconv.ParseInt(parts[1], 10, 64)
				if parseErr == nil {
					totalBytes = val
				}
			}
		}
	}

	if totalBytes <= 0 {
		return c.downloadLinear(ctx, urlStr, destPath, progressFn)
	}

	file, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return 0, err
	}
	defer file.Close()

	if chunkSize < 64*1024 {
		chunkSize = 64 * 1024
	}

	var chunks []rangeChunk
	for start := int64(0); start < totalBytes; start += chunkSize {
		end := start + chunkSize - 1
		if end >= totalBytes {
			end = totalBytes - 1
		}
		chunks = append(chunks, rangeChunk{start: start, end: end})
	}

	chunkChan := make(chan rangeChunk, len(chunks))
	for _, ch := range chunks {
		chunkChan <- ch
	}
	close(chunkChan)

	if workers <= 0 {
		workers = 4
	}
	if workers > 32 {
		workers = 32
	}

	var downloadedBytes atomic.Int64
	tStart := time.Now()

	errChan := make(chan error, workers)
	var wg sync.WaitGroup

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			workerClient := &http.Client{Timeout: 30 * time.Second}
			for {
				select {
				case <-ctx.Done():
					errChan <- ctx.Err()
					return
				case chunk, ok := <-chunkChan:
					if !ok {
						return
					}
					req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
					if err != nil {
						errChan <- err
						return
					}
					req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", chunk.start, chunk.end))
					req.Header.Set("User-Agent", "Mozilla/5.0")

					resp, err := workerClient.Do(req)
					if err != nil {
						errChan <- err
						return
					}

					data, err := io.ReadAll(resp.Body)
					resp.Body.Close()
					if err != nil {
						errChan <- err
						return
					}

					_, err = file.WriteAt(data, chunk.start)
					if err != nil {
						errChan <- err
						return
					}

					curr := downloadedBytes.Add(int64(len(data)))
					if progressFn != nil {
						elapsed := time.Since(tStart).Seconds()
						speed := 0.0
						if elapsed > 0 {
							speed = (float64(curr) / (1024.0 * 1024.0)) / elapsed
						}
						pct := (float64(curr) / float64(totalBytes)) * 100.0
						progressFn(DownloadProgress{
							DownloadedBytes: curr,
							TotalBytes:      totalBytes,
							Percent:         math.Round(pct*10) / 10,
							SpeedMbps:       math.Round(speed*100) / 100,
						})
					}
				}
			}
		}()
	}

	wg.Wait()
	close(errChan)

	for e := range errChan {
		if e != nil {
			return 0, e
		}
	}

	return downloadedBytes.Load(), nil
}

func (c *Client) downloadLinear(
	ctx context.Context,
	urlStr string,
	destPath string,
	progressFn ProgressFunc,
) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Range", "bytes=0-")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return 0, fmt.Errorf("unexpected status downloading audio: %d", resp.StatusCode)
	}

	totalBytes := resp.ContentLength

	file, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return 0, err
	}
	defer file.Close()

	buf := make([]byte, 64*1024)
	var downloaded int64 = 0
	tStart := time.Now()

	for {
		select {
		case <-ctx.Done():
			return downloaded, ctx.Err()
		default:
		}

		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			_, writeErr := file.Write(buf[:n])
			if writeErr != nil {
				return downloaded, writeErr
			}
			downloaded += int64(n)
			if progressFn != nil && totalBytes > 0 {
				elapsed := time.Since(tStart).Seconds()
				speed := 0.0
				if elapsed > 0 {
					speed = (float64(downloaded) / (1024.0 * 1024.0)) / elapsed
				}
				pct := (float64(downloaded) / float64(totalBytes)) * 100.0
				progressFn(DownloadProgress{
					DownloadedBytes: downloaded,
					TotalBytes:      totalBytes,
					Percent:         math.Round(pct*10) / 10,
					SpeedMbps:       math.Round(speed*100) / 100,
				})
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			return downloaded, readErr
		}
	}

	return downloaded, nil
}
