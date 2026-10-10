package typetube

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/playeon/typetube-go/tap"
)

const (
	DefaultHost    = "tap://sg.clusters.typetube.xyz:8443"
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
	tapClient  *tap.Client
}

func NewClient(opts ...Option) *Client {
	c := &Client{
		apiKey:  "",
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

	// Initialize TAP client if host is a tap cluster or host/domain
	c.initTapClient()

	return c
}

func (c *Client) initTapClient() {
	target := c.host
	port := 8443

	if strings.HasPrefix(target, "tap://") || strings.Contains(target, "clusters.typetube.xyz") || !strings.HasPrefix(target, "http") {
		host, p := tap.ParseTapHost(target, port)
		c.tapClient = tap.NewClient(host, p, c.apiKey, c.timeout)
	}
}

func (c *Client) Ping(ctx context.Context) (time.Duration, error) {
	if c.tapClient != nil {
		return c.tapClient.Ping(ctx)
	}
	t0 := time.Now()
	_, err := c.Usage(ctx)
	if err != nil {
		return 0, err
	}
	return time.Since(t0), nil
}

func (c *Client) Resolve(ctx context.Context, query string) (*TrackResult, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, errors.New("query cannot be empty")
	}

	if c.tapClient != nil {
		rawBytes, err := c.tapClient.Invoke(ctx, "resolve", []byte(q))
		if err != nil {
			return nil, err
		}
		return decodeTrackResult(rawBytes)
	}

	// Fallback to HTTP API
	params := url.Values{}
	params.Set("q", q)
	params.Set("format", c.format)

	reqURL := fmt.Sprintf("%s/v1/resolve?%s", c.host, params.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/x-protobuf, application/octet-stream;q=0.9, */*;q=0.8")
	if c.apiKey != "" {
		req.Header.Set("X-API-Key", c.apiKey)
	}

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
		return nil, &TypeTubeError{Message: errMsg, StatusCode: resp.StatusCode}
	}

	contentType := resp.Header.Get("Content-Type")
	trimmed := strings.TrimSpace(string(rawBytes))
	if strings.HasPrefix(trimmed, "{") || strings.Contains(contentType, "application/json") {
		var result TrackResult
		if err := json.Unmarshal(rawBytes, &result); err != nil {
			return nil, err
		}
		return &result, nil
	}

	return decodeTrackResult(rawBytes)
}

func (c *Client) Search(ctx context.Context, query string, limit int) ([]SearchItem, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, errors.New("search query cannot be empty")
	}
	if limit <= 0 {
		limit = 10
	}

	if c.tapClient != nil {
		payload := []byte(fmt.Sprintf("%s\x00%d", q, limit))
		rawBytes, err := c.tapClient.Invoke(ctx, "search", payload)
		if err != nil {
			return nil, err
		}
		return decodeSearchResults(rawBytes)
	}

	// Fallback to HTTP API
	params := url.Values{}
	params.Set("q", q)
	params.Set("limit", strconv.Itoa(limit))
	params.Set("format", c.format)

	reqURL := fmt.Sprintf("%s/v1/search?%s", c.host, params.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/x-protobuf, application/octet-stream;q=0.9, */*;q=0.8")
	if c.apiKey != "" {
		req.Header.Set("X-API-Key", c.apiKey)
	}

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

	return decodeSearchResults(rawBytes)
}

func (c *Client) Usage(ctx context.Context) (*UsageInfo, error) {
	if c.tapClient != nil {
		rawBytes, err := c.tapClient.Invoke(ctx, "usage", nil)
		if err == nil {
			var usage UsageInfo
			if jsonErr := json.Unmarshal(rawBytes, &usage); jsonErr == nil {
				return &usage, nil
			}
		}
	}

	reqURL := fmt.Sprintf("%s/v1/usage", c.host)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if c.apiKey != "" {
		req.Header.Set("X-API-Key", c.apiKey)
	}

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

func (c *Client) DownloadAudio(
	ctx context.Context,
	trackOrQuery interface{},
	dest string,
	cfg ...DownloadConfig,
) (*DownloadResult, error) {
	var track *TrackResult
	switch v := trackOrQuery.(type) {
	case *TrackResult:
		track = v
	case TrackResult:
		track = &v
	case string:
		var err error
		track, err = c.Resolve(ctx, v)
		if err != nil {
			return nil, err
		}
	default:
		return nil, errors.New("trackOrQuery must be a string query/URL or *TrackResult")
	}

	config := DownloadConfig{
		Workers:        6,
		ChunkSizeBytes: 10 * 1024 * 1024,
		Quality:        "highest",
	}
	if len(cfg) > 0 {
		if cfg[0].Workers > 0 {
			config.Workers = cfg[0].Workers
		}
		if cfg[0].ChunkSizeBytes > 0 {
			config.ChunkSizeBytes = cfg[0].ChunkSizeBytes
		}
		if cfg[0].Quality != "" {
			config.Quality = cfg[0].Quality
		}
		config.ProgressCallback = cfg[0].ProgressCallback
	}

	stream := c.selectAudioStream(track, config.Quality)
	if stream == nil || stream.URL == "" {
		return nil, errors.New("no playable audio stream available")
	}

	finalDest := c.resolveDestination(dest, track, "m4a")

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

func (c *Client) DownloadVideo(
	ctx context.Context,
	trackOrQuery interface{},
	dest string,
	cfg ...DownloadConfig,
) (*DownloadResult, error) {
	var track *TrackResult
	switch v := trackOrQuery.(type) {
	case *TrackResult:
		track = v
	case TrackResult:
		track = &v
	case string:
		var err error
		track, err = c.Resolve(ctx, v)
		if err != nil {
			return nil, err
		}
	default:
		return nil, errors.New("trackOrQuery must be a string query/URL or *TrackResult")
	}

	config := DownloadConfig{
		Workers:        6,
		ChunkSizeBytes: 10 * 1024 * 1024,
		Quality:        "1080p",
	}
	if len(cfg) > 0 {
		if cfg[0].Workers > 0 {
			config.Workers = cfg[0].Workers
		}
		if cfg[0].ChunkSizeBytes > 0 {
			config.ChunkSizeBytes = cfg[0].ChunkSizeBytes
		}
		if cfg[0].Quality != "" {
			config.Quality = cfg[0].Quality
		}
		config.ProgressCallback = cfg[0].ProgressCallback
	}

	stream := c.selectVideoStream(track, config.Quality)
	if stream == nil || stream.URL == "" {
		return nil, errors.New("no playable video stream available")
	}

	ext := "mp4"
	if strings.Contains(stream.MimeType, "webm") {
		ext = "webm"
	}
	finalDest := c.resolveDestination(dest, track, ext)

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

func (c *Client) DownloadMedia(
	ctx context.Context,
	trackOrQuery interface{},
	dest string,
	cfg ...DownloadConfig,
) (*DownloadResult, error) {
	var track *TrackResult
	switch v := trackOrQuery.(type) {
	case *TrackResult:
		track = v
	case TrackResult:
		track = &v
	case string:
		var err error
		track, err = c.Resolve(ctx, v)
		if err != nil {
			return nil, err
		}
	default:
		return nil, errors.New("trackOrQuery must be a string query/URL or *TrackResult")
	}

	config := DownloadConfig{
		Workers:        6,
		ChunkSizeBytes: 10 * 1024 * 1024,
		Quality:        "1080p",
	}
	if len(cfg) > 0 {
		if cfg[0].Workers > 0 {
			config.Workers = cfg[0].Workers
		}
		if cfg[0].ChunkSizeBytes > 0 {
			config.ChunkSizeBytes = cfg[0].ChunkSizeBytes
		}
		if cfg[0].Quality != "" {
			config.Quality = cfg[0].Quality
		}
		config.ProgressCallback = cfg[0].ProgressCallback
	}

	vStream := c.selectVideoStream(track, config.Quality)
	aStream := c.selectAudioStream(track, "highest")

	if vStream == nil || vStream.URL == "" {
		return nil, errors.New("no playable video stream available")
	}
	if aStream == nil || aStream.URL == "" {
		return nil, errors.New("no playable audio stream available")
	}

	finalDest := c.resolveDestination(dest, track, "mp4")
	tempV := fmt.Sprintf("%s.tmp.%d.v", finalDest, time.Now().UnixNano())
	tempA := fmt.Sprintf("%s.tmp.%d.a", finalDest, time.Now().UnixNano())
	defer os.Remove(tempV)
	defer os.Remove(tempA)

	var vDownloaded, aDownloaded, vTotal, aTotal atomic.Int64
	tStart := time.Now()

	notifyCombined := func() {
		if config.ProgressCallback == nil {
			return
		}
		tot := vTotal.Load() + aTotal.Load()
		curr := vDownloaded.Load() + aDownloaded.Load()
		pct := 0.0
		if tot > 0 {
			pct = (float64(curr) / float64(tot)) * 100.0
		}
		elapsed := time.Since(tStart).Seconds()
		speed := 0.0
		if elapsed > 0 {
			speed = (float64(curr) / (1024.0 * 1024.0)) / elapsed
		}
		config.ProgressCallback(DownloadProgress{
			DownloadedBytes: curr,
			TotalBytes:      tot,
			Percent:         math.Round(pct*10) / 10,
			SpeedMbps:       math.Round(speed*100) / 100,
		})
	}

	var wg sync.WaitGroup
	var vErr, aErr error

	wg.Add(2)
	go func() {
		defer wg.Done()
		_, vErr = c.downloadRangeParallel(ctx, vStream.URL, tempV, config.Workers, config.ChunkSizeBytes, func(p DownloadProgress) {
			vDownloaded.Store(p.DownloadedBytes)
			vTotal.Store(p.TotalBytes)
			notifyCombined()
		})
	}()

	go func() {
		defer wg.Done()
		_, aErr = c.downloadRangeParallel(ctx, aStream.URL, tempA, config.Workers, config.ChunkSizeBytes, func(p DownloadProgress) {
			aDownloaded.Store(p.DownloadedBytes)
			aTotal.Store(p.TotalBytes)
			notifyCombined()
		})
	}()

	wg.Wait()

	if vErr != nil {
		return nil, vErr
	}
	if aErr != nil {
		return nil, aErr
	}

	// Mux using ffmpeg
	args := []string{
		"-y",
		"-i", tempV,
		"-i", tempA,
		"-c:v", "copy",
		"-c:a", "copy",
		"-movflags", "+faststart",
		finalDest,
	}
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("ffmpeg muxing failed: %s (%w)", string(out), err)
	}

	fi, err := os.Stat(finalDest)
	var finalSize int64
	if err == nil {
		finalSize = fi.Size()
	}

	durSec := time.Since(tStart).Seconds()
	speedMbps := 0.0
	if durSec > 0 {
		speedMbps = (float64(finalSize) / (1024.0 * 1024.0)) / durSec
	}
	fileSizeMB := float64(finalSize) / (1024.0 * 1024.0)

	return &DownloadResult{
		Success:         true,
		FilePath:        finalDest,
		FileName:        filepath.Base(finalDest),
		SizeBytes:       finalSize,
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

func (c *Client) resolveDestination(dest string, track *TrackResult, ext string) string {
	finalDest := dest
	if finalDest == "" {
		finalDest = "./downloads"
	}

	stat, err := os.Stat(finalDest)
	isDir := (err == nil && stat.IsDir()) || filepath.Ext(finalDest) == ""

	if isDir {
		_ = os.MkdirAll(finalDest, 0755)
		reg := regexp.MustCompile(`[^a-zA-Z0-9 ._-]`)
		baseName := fmt.Sprintf("%s - %s", track.Author, track.Title)
		safeName := strings.TrimSpace(reg.ReplaceAllString(baseName, ""))
		return filepath.Join(finalDest, fmt.Sprintf("%s.%s", safeName, ext))
	}

	parentDir := filepath.Dir(finalDest)
	if parentDir != "" {
		_ = os.MkdirAll(parentDir, 0755)
	}
	return finalDest
}

func (c *Client) selectAudioStream(track *TrackResult, quality string) *AudioStream {
	if len(track.AudioStreams) == 0 {
		return track.BestAudio
	}

	var usableStreams []AudioStream
	for _, s := range track.AudioStreams {
		if !strings.Contains(s.URL, "c=WEB_CREATOR") {
			usableStreams = append(usableStreams, s)
		}
	}
	if len(usableStreams) == 0 {
		usableStreams = track.AudioStreams
	}

	switch strings.ToLower(quality) {
	case "hifi", "256kbps", "highest":
		for i := range usableStreams {
			if usableStreams[i].ITag == 141 || usableStreams[i].ITag == 774 {
				return &usableStreams[i]
			}
		}
	case "128kbps":
		for i := range usableStreams {
			if usableStreams[i].ITag == 140 || usableStreams[i].ITag == 251 {
				return &usableStreams[i]
			}
		}
	}

	if track.BestAudio != nil && !strings.Contains(track.BestAudio.URL, "c=WEB_CREATOR") {
		return track.BestAudio
	}
	return &usableStreams[0]
}

func (c *Client) selectVideoStream(track *TrackResult, quality string) *VideoStream {
	if len(track.VideoStreams) == 0 {
		return track.BestVideo
	}

	targetHeight := 1080
	if quality != "" {
		digits := ""
		for _, ch := range quality {
			if ch >= '0' && ch <= '9' {
				digits += string(ch)
			}
		}
		if val, err := strconv.Atoi(digits); err == nil && val > 0 {
			targetHeight = val
		}
	}

	parseHeight := func(q string) int {
		digits := ""
		for _, ch := range q {
			if ch >= '0' && ch <= '9' {
				digits += string(ch)
			}
		}
		val, _ := strconv.Atoi(digits)
		return val
	}

	for i := range track.VideoStreams {
		if parseHeight(track.VideoStreams[i].Quality) <= targetHeight {
			return &track.VideoStreams[i]
		}
	}

	if track.BestVideo != nil {
		return track.BestVideo
	}
	return &track.VideoStreams[0]
}

type rangeChunk struct {
	start int64
	end   int64
}

func (c *Client) makeIPv4Transport() *http.Transport {
	dialer := &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	return &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialer.DialContext(ctx, "tcp4", addr)
		},
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: false,
		},
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 20,
		IdleConnTimeout:     90 * time.Second,
	}
}

func (c *Client) downloadRangeParallel(
	ctx context.Context,
	urlStr string,
	destPath string,
	workers int,
	chunkSize int64,
	progressFn ProgressFunc,
) (int64, error) {
	transport := c.makeIPv4Transport()
	probeClient := &http.Client{
		Transport: transport,
		Timeout:   10 * time.Second,
	}

	probeReq, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		return 0, err
	}
	probeReq.Header.Set("Range", "bytes=0-0")
	probeReq.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0.0.0 Safari/537.36")

	probeResp, err := probeClient.Do(probeReq)
	var totalBytes int64 = 0
	if err == nil {
		defer probeResp.Body.Close()
		if probeResp.StatusCode == http.StatusForbidden {
			return 0, &TypeTubeError{
				Message:    "Stream download forbidden (HTTP 403): track restricted to the cluster's region",
				StatusCode: 403,
			}
		}
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

	if chunkSize < 512*1024 {
		chunkSize = 10 * 1024 * 1024
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
			workerClient := &http.Client{
				Transport: transport,
				Timeout:   60 * time.Second,
			}
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
					req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0.0.0 Safari/537.36")

					resp, err := workerClient.Do(req)
					if err != nil {
						errChan <- err
						return
					}

					if resp.StatusCode == http.StatusForbidden {
						resp.Body.Close()
						errChan <- &TypeTubeError{
							Message:    "Stream download forbidden (HTTP 403): track restricted to the cluster's region",
							StatusCode: 403,
						}
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
	transport := c.makeIPv4Transport()
	httpClient := &http.Client{
		Transport: transport,
		Timeout:   60 * time.Second,
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0.0.0 Safari/537.36")
	req.Header.Set("Range", "bytes=0-")

	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden {
		return 0, &TypeTubeError{
			Message:    "Stream download forbidden (HTTP 403): track restricted to the cluster's region",
			StatusCode: 403,
		}
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return 0, &TypeTubeError{
			Message:    fmt.Sprintf("Download stream failed with status %d", resp.StatusCode),
			StatusCode: resp.StatusCode,
		}
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

func (c *Client) Close() error {
	if c.tapClient != nil {
		return c.tapClient.Close()
	}
	return nil
}
