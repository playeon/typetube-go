# typetube-go

Official Go client library for the TypeTube streaming API.

Zero external dependencies. Implemented entirely with the Go standard library (net/http, encoding/binary, sync, os).

## Features

- Native Protocol Buffers wire decoding (fast binary responses from /v1/resolve and /v1/search)
- Concurrent chunked range downloading using goroutines with automatic linear fallback
- High precision stream resolution (/v1/resolve)
- Video search (/v1/search)
- Quota and usage inspection (/v1/usage)
- Configurable timeouts, custom endpoints, and API key authentication

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

	"github.com/playeon/typetube-go"
)

func main() {
	client := typetube.NewClient(
		typetube.WithHost("https://typetube.xysushi.in"),
		typetube.WithAPIKey("your_api_key_here"),
	)

	ctx := context.Background()

	track, err := client.Resolve(ctx, "Rick Astley - Never Gonna Give You Up")
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Resolved: %s by %s (%ds)\n", track.Title, track.Author, track.DurationSeconds)
	if track.BestAudio != nil {
		fmt.Printf("Best Audio URL: %s\n", track.BestAudio.URL)
	}

	res, err := client.DownloadAudio(ctx, track, "track.m4a", typetube.DownloadConfig{
		Workers: 8,
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Downloaded %d bytes to %s in %.2fs (%.2f MB/s)\n",
		res.SizeBytes, res.FilePath, res.DurationSec, res.SpeedMbps)
}
```

## Usage

### Stream Resolution

Resolve YouTube videos by search query, video ID, or direct URL:

```go
track, err := client.Resolve(ctx, "https://www.youtube.com/watch?v=dQw4w9WgXcQ")
if err != nil {
	log.Fatal(err)
}

fmt.Printf("Title: %s\n", track.Title)
fmt.Printf("Author: %s\n", track.Author)
fmt.Printf("Duration: %d seconds\n", track.DurationSeconds)
if track.BestAudio != nil {
	fmt.Printf("Bitrate: %d bps\n", track.BestAudio.Bitrate)
	fmt.Printf("MIME: %s\n", track.BestAudio.MimeType)
}
```

### Video Search

Search for tracks with custom limits:

```go
results, err := client.Search(ctx, "Hans Zimmer Interstellar", 5)
if err != nil {
	log.Fatal(err)
}

for _, item := range results {
	fmt.Printf("%s (%ds) - %s\n", item.Title, item.Duration, item.URL)
}
```

### Concurrent Chunked Audio Download

Download audio files using parallel HTTP range requests with configurable worker counts and chunk sizes:

```go
cfg := typetube.DownloadConfig{
	Workers:        8,
	ChunkSizeBytes: 1024 * 1024,
	Quality:        "highest",
	ProgressCallback: func(p typetube.DownloadProgress) {
		fmt.Printf("\rProgress: %.1f%% (%d/%d bytes) at %.2f MB/s",
			p.Percent, p.DownloadedBytes, p.TotalBytes, p.SpeedMbps)
	},
}

dl, err := client.DownloadAudio(ctx, track, "interstellar.m4a", cfg)
if err != nil {
	log.Fatal(err)
}

fmt.Printf("\nCompleted: %s (%.2f MB)\n", dl.FilePath, dl.FileSizeMB)
```

### Quota and Usage

Check quota limits and remaining calls:

```go
usage, err := client.Usage(ctx)
if err != nil {
	log.Fatal(err)
}

fmt.Printf("Tier: %s\n", usage.KeyType)
fmt.Printf("Remaining: %d/%d\n", usage.Remaining, usage.Limit)
fmt.Printf("Reset in: %d seconds\n", usage.ResetInSeconds)
```

## License

MIT
