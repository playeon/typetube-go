# typetube-go

High-performance TypeTube streaming client for Go with native TAP binary protocol support and concurrent range downloads.

## Features

- **TAP Binary Protocol**: Direct TCP binary framing with sequence IDs, cryptographic session negotiation (ECDH P-256 + AES-256-GCM), and low latency multiplexing.
- **Pure Go Compression**: Zstandard (`zstd`), Brotli, and Deflate stream decompression.
- **Multi-Worker Range Downloader**: High-speed chunked downloads with goroutine worker pools and direct sparse writes (`os.File.WriteAt`).
- **Media Download & Muxing**: `DownloadAudio`, `DownloadVideo`, and combined `DownloadMedia` with automated FFmpeg muxing.
- **IPv4 Socket Binding**: Dedicated dialer enforcing IPv4 TCP connections to avoid IPv6 rate-limiting.
- **Regional 403 Error Handling**: Clear error responses when streams are restricted to the resolver cluster's region.

## Installation

```bash
go get github.com/playeon/typetube-go
```

## Quick Start

```go
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/playeon/typetube-go"
)

func main() {
	// Connect directly to a TAP cluster
	client := typetube.NewClient(
		typetube.WithHost("tap://sg.clusters.typetube.xyz:8443"),
		typetube.WithAPIKey("your_api_key_here"),
		typetube.WithTimeout(15*time.Second),
	)
	defer client.Close()

	ctx := context.Background()

	// Measure TAP ping latency
	latency, err := client.Ping(ctx)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Cluster ping latency: %v\n", latency)

	// Resolve track
	track, err := client.Resolve(ctx, "Rick Astley - Never Gonna Give You Up")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Resolved: %s by %s (%ds)\n", track.Title, track.Author, track.DurationSeconds)

	// High-speed chunked audio download
	dl, err := client.DownloadAudio(ctx, track, "./downloads", typetube.DownloadConfig{
		Workers: 6,
		ProgressCallback: func(p typetube.DownloadProgress) {
			fmt.Printf("\rDownload: %.1f%% (%.2f MB/s)", p.Percent, p.SpeedMbps)
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("\nSaved to %s (%d bytes in %.2fs)\n", dl.FilePath, dl.SizeBytes, dl.DurationSec)
}
```

## Usage

### Stream Resolution

Pass either a search query, YouTube video ID, or video URL:

```go
track, err := client.Resolve(ctx, "https://www.youtube.com/watch?v=dQw4w9WgXcQ")
if err != nil {
	log.Fatal(err)
}

fmt.Printf("Title: %s\n", track.Title)
fmt.Printf("Author: %s\n", track.Author)
fmt.Printf("Duration: %d seconds\n", track.DurationSeconds)
if track.BestAudio != nil {
	fmt.Printf("Bitrate: %d bps, Quality: %s\n", track.BestAudio.Bitrate, track.BestAudio.Quality)
}
```

### Video Search

```go
results, err := client.Search(ctx, "Hans Zimmer Interstellar", 5)
if err != nil {
	log.Fatal(err)
}

for _, item := range results {
	fmt.Printf("%s (%ds) - %s\n", item.Title, item.Duration, item.URL)
}
```

### Audio, Video, and Media Downloads

All download methods accept either a search query string or a pre-resolved `*TrackResult`:

```go
// 1. Audio download (m4a / opus)
dlAudio, err := client.DownloadAudio(ctx, track, "audio.m4a", typetube.DownloadConfig{
	Workers: 6,
	Quality: "highest", // "highest", "hifi", "128kbps"
})

// 2. Video download (mp4 / webm)
dlVideo, err := client.DownloadVideo(ctx, track, "video.mp4", typetube.DownloadConfig{
	Workers: 6,
	Quality: "1080p", // "1080p", "720p", "480p", "360p"
})

// 3. Combined Video + Audio muxed with FFmpeg
dlMedia, err := client.DownloadMedia(ctx, track, "media.mp4", typetube.DownloadConfig{
	Workers: 6,
	Quality: "1080p",
})
```

### Regional Restriction Handling

When a track stream URL is geographically restricted by Google CDN to the resolver cluster's region, the client returns a clean error:

```text
Stream download forbidden (HTTP 403): track restricted to the cluster's region
```

Connect to an alternative cluster (such as `tap://sg.clusters.typetube.xyz` or `tap://nl.clusters.typetube.xyz`) when encountering regional blocks.

## License

MIT
