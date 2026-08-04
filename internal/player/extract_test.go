package player

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeYtdlp writes a shell script to a temp dir and puts it on PATH as
// "yt-dlp". The script's stdout becomes extractURL's returned URL (when exit
// code is 0) and its stderr becomes the error message (when exit code != 0).
// t.TempDir() is cleaned up automatically, including the fake binary, so each
// test gets a pristine PATH.
//
// ytdlpPath is a sync.OnceValues cached at package init, so without resetting
// it the first test's LookPath result would persist for all later tests —
// including ones that change PATH. We reassign it to a fresh OnceValues so
// each test gets a clean lookup.
func fakeYtdlp(t *testing.T, script string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "yt-dlp")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	ytdlpPath = sync.OnceValues(func() (string, error) { return exec.LookPath("yt-dlp") })
}

// TestExtractURLSuccess: a successful yt-dlp run returns the trimmed stdout as
// the stream URL with resolved=true. This is the happy path — every track
// change goes through it on a cache miss.
func TestExtractURLSuccess(t *testing.T) {
	fakeYtdlp(t, `echo "https://stream.example/x"`)

	url, resolved, err := extractURL(context.Background(), "dQw4w9WgXcQ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resolved {
		t.Fatal("resolved must be true on success")
	}
	if url != "https://stream.example/x" {
		t.Fatalf("url = %q, want %q", url, "https://stream.example/x")
	}
}

// TestExtractURLNotFound: when yt-dlp is absent from PATH, extractURL must
// return errNoYtdlp specifically (not a generic "exec: ..." error), so the TUI
// can show "install yt-dlp" instead of a confusing system error. This is the
// only error path the TUI translates into an actionable message.
func TestExtractURLNotFound(t *testing.T) {
	// Empty PATH — no yt-dlp, nothing.
	t.Setenv("PATH", t.TempDir())
	// Reset the cached lookpath — without this, the result from a previous
	// test's fakeYtdlp would still be cached and mask the "not found" path.
	ytdlpPath = sync.OnceValues(func() (string, error) { return exec.LookPath("yt-dlp") })

	_, _, err := extractURL(context.Background(), "dQw4w9WgXcQ")
	if !errors.Is(err, errNoYtdlp) {
		t.Fatalf("error = %v, want errNoYtdlp", err)
	}
}

// TestExtractURLFailureSurfacesStderr: yt-dlp returns a human-readable reason
// on failure (age-gate, geo-block, bot check). extractURL must surface that
// reason — not a generic "exit status 1" — so the TUI can show the user
// something actionable.
func TestExtractURLFailureSurfacesStderr(t *testing.T) {
	fakeYtdlp(t, `echo "ERROR: video unavailable" >&2; exit 1`)

	_, resolved, err := extractURL(context.Background(), "dQw4w9WgXcQ")
	if resolved {
		t.Fatal("resolved must be false on failure")
	}
	if err == nil {
		t.Fatal("expected an error on failure")
	}
	if !strings.Contains(err.Error(), "video unavailable") {
		t.Fatalf("error = %q, want it to contain 'video unavailable'", err.Error())
	}
}

// TestExtractURLCancellation: a cancelled context must make extractURL return
// promptly (not wait the full loadTimeout = 60s). Without this, a user who
// skips quickly through a queue would be stuck waiting for each abandoned
// resolve to time out before the next skip becomes responsive.
func TestExtractURLCancellation(t *testing.T) {
	fakeYtdlp(t, `sleep 5`)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, resolved, err := extractURL(ctx, "dQw4w9WgXcQ")
	if resolved {
		t.Fatal("resolved must be false on cancellation")
	}
	if err == nil {
		t.Fatal("expected an error on cancellation")
	}
}

// TestExtractURLInvalidVideoID: a videoID with characters outside the YouTube
// alphabet must be rejected before any subprocess is spawned. The id is
// interpolated into a URL and passed to yt-dlp; an unvalidated id could
// smuggle extra URL components or argv content into the subprocess.
func TestExtractURLInvalidVideoID(t *testing.T) {
	// Even with a working yt-dlp, invalid ids must not reach it.
	fakeYtdlp(t, `echo "should not be called"`)

	_, resolved, err := extractURL(context.Background(), "has spaces")
	if resolved {
		t.Fatal("resolved must be false for invalid video id")
	}
	if err == nil {
		t.Fatal("expected an error for invalid video id")
	}
}

// TestExtractURLEmptyOutput: yt-dlp returning an empty URL (no stdout) must be
// treated as a failure. An empty URL handed to mpv would cause it to error out
// with a confusing message the user can't act on.
func TestExtractURLEmptyOutput(t *testing.T) {
	fakeYtdlp(t, `echo ""`)

	_, resolved, err := extractURL(context.Background(), "dQw4w9WgXcQ")
	if resolved {
		t.Fatal("resolved must be false on empty output")
	}
	if err == nil {
		t.Fatal("expected an error on empty output")
	}
}

// TestExtractURLTrimsWhitespace: yt-dlp's output may carry a trailing newline.
// extractURL must trim it — an untrimmed URL with a newline would fail to
// connect because mpv would try to open a URL ending in \n.
func TestExtractURLTrimsWhitespace(t *testing.T) {
	// echo adds a newline; the script returns "url\n" which must be trimmed.
	fakeYtdlp(t, `echo "https://stream.example/trimmed"`)

	url, resolved, err := extractURL(context.Background(), "dQw4w9WgXcQ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resolved {
		t.Fatal("resolved must be true")
	}
	if url != "https://stream.example/trimmed" {
		t.Fatalf("url = %q, want %q (trailing newline must be trimmed)", url, "https://stream.example/trimmed")
	}
}
