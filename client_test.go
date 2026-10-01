package typetube_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/playeon/typetube-go"
)

func TestClientEndToEnd(t *testing.T) {
	client := typetube.NewClient(
		typetube.WithHost("https://typetube.xysushi.in"),
		typetube.WithAPIKey("tt_priv_prod_key"),
		typetube.WithTimeout(15*time.Second),
	)

	ctx := context.Background()

	t.Run("Usage", func(t *testing.T) {
		usage, err := client.Usage(ctx)
		if err != nil {
			t.Fatalf("Usage failed: %v", err)
		}
		if !usage.Success {
			t.Fatalf("expected usage success true, got %v", usage.Success)
		}
		if usage.Limit <= 0 {
			t.Fatalf("expected positive limit, got %d", usage.Limit)
		}
		t.Logf("usage: tier=%s remaining=%d/%d", usage.KeyType, usage.Remaining, usage.Limit)
	})

	t.Run("Search", func(t *testing.T) {
		results, err := client.Search(ctx, "Queen Bohemian Rhapsody", 3)
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}
		if len(results) == 0 {
			t.Fatalf("expected at least 1 search result, got %d", len(results))
		}
		t.Logf("search: found %d items, first='%s' (%s)", len(results), results[0].Title, results[0].URL)
	})

	var track *typetube.TrackResult
	t.Run("Resolve", func(t *testing.T) {
		res, err := client.Resolve(ctx, "dQw4w9WgXcQ")
		if err != nil {
			t.Fatalf("Resolve failed: %v", err)
		}
		if !res.Success {
			t.Fatalf("expected resolve success true, got %v", res.Success)
		}
		if res.BestAudio == nil || res.BestAudio.URL == "" {
			t.Fatalf("expected non-nil best audio with valid URL")
		}
		track = res
		t.Logf("resolve: title='%s', author='%s', duration=%ds, latency=%.1fms", res.Title, res.Author, res.DurationSeconds, res.LatencyMs)
	})

	t.Run("DownloadAudio", func(t *testing.T) {
		if track == nil {
			t.Skip("skipping download test because resolve failed")
		}
		outPath := "/tmp/go_test_audio.m4a"
		_ = os.Remove(outPath)

		dl, err := client.DownloadAudio(ctx, track, outPath, typetube.DownloadConfig{
			Workers:        4,
			ChunkSizeBytes: 512 * 1024,
		})
		if err != nil {
			t.Fatalf("DownloadAudio failed: %v", err)
		}

		defer os.Remove(outPath)

		if dl.SizeBytes <= 100000 {
			t.Fatalf("downloaded file too small: %d bytes", dl.SizeBytes)
		}

		stat, err := os.Stat(outPath)
		if err != nil {
			t.Fatalf("downloaded file stat error: %v", err)
		}
		if stat.Size() != dl.SizeBytes {
			t.Fatalf("stat size %d does not match dl.SizeBytes %d", stat.Size(), dl.SizeBytes)
		}

		t.Logf("download: %d bytes in %.2fs (%.2f MB/s) to %s", dl.SizeBytes, dl.DurationSec, dl.SpeedMbps, dl.FilePath)
	})
}
