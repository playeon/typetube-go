package typetube_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/playeon/typetube-go"
)

func TestTapSingaporeCluster(t *testing.T) {
	client := typetube.NewClient(
		typetube.WithHost("tap://sg.clusters.typetube.xyz:8443"),
		typetube.WithAPIKey("tt_priv_audium_master"),
		typetube.WithTimeout(15*time.Second),
	)
	defer client.Close()

	ctx := context.Background()

	t.Run("Ping", func(t *testing.T) {
		latency, err := client.Ping(ctx)
		if err != nil {
			t.Fatalf("Ping failed: %v", err)
		}
		t.Logf("Singapore cluster latency: %v", latency)
	})

	t.Run("Resolve", func(t *testing.T) {
		res, err := client.Resolve(ctx, "dQw4w9WgXcQ")
		if err != nil {
			t.Fatalf("Resolve over TAP failed: %v", err)
		}
		if res.Title == "" {
			t.Fatalf("empty title returned")
		}
		t.Logf("Resolved via TAP: %s (author: %s, duration: %ds)", res.Title, res.Author, res.DurationSeconds)
	})
}

func TestRegional403Handling(t *testing.T) {
	client := typetube.NewClient(
		typetube.WithHost("tap://nl.clusters.typetube.xyz:8443"),
		typetube.WithAPIKey("tt_priv_audium_master"),
		typetube.WithTimeout(15*time.Second),
	)
	defer client.Close()

	ctx := context.Background()

	res, err := client.Resolve(ctx, "sabrina carpenter espresso")
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	_, dlErr := client.DownloadAudio(ctx, res, "/tmp/espresso_test.m4a")
	if dlErr == nil {
		t.Fatalf("expected 403 regional restriction error, got nil")
	}

	expectedSub := "Stream download forbidden (HTTP 403): track restricted to the cluster's region"
	if !strings.Contains(dlErr.Error(), expectedSub) {
		t.Fatalf("expected error containing %q, got %q", expectedSub, dlErr.Error())
	}
	t.Logf("Successfully caught clean 403 error: %v", dlErr)
}
