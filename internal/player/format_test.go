package player

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestExtractURLPrefersSegmentedStream pins the format-selector order.
// YouTube answers 403 to the large, unbounded GETs ffmpeg issues against
// progressive googlevideo URLs unless the request carries a proof-of-origin
// token, so a plain "bestaudio" resolve succeeds and then fails to play —
// exactly the "stream error" the retry path cannot recover from. HLS is served
// in small segments and stays playable, so it must be asked for first, with
// progressive audio kept only as a last resort.
func TestExtractURLPrefersSegmentedStream(t *testing.T) {
	args := filepath.Join(t.TempDir(), "args")
	fakeYtdlp(t, `printf '%s\n' "$@" > `+args+`
echo "https://stream.example/x"`)

	if _, _, err := extractURL(context.Background(), "dQw4w9WgXcQ"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(args)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Split(strings.TrimSpace(string(raw)), "\n")

	i := indexOf(got, "-f")
	if i < 0 || i+1 >= len(got) {
		t.Fatalf("no -f selector in yt-dlp args: %v", got)
	}
	selector := got[i+1]
	hls := strings.Index(selector, "m3u8")
	progressive := strings.LastIndex(selector, "bestaudio")
	if hls < 0 {
		t.Fatalf("selector %q never asks for a segmented (m3u8) stream", selector)
	}
	if progressive < hls {
		t.Fatalf("selector %q prefers progressive audio over HLS", selector)
	}
}

func indexOf(args []string, want string) int {
	for i, a := range args {
		if a == want {
			return i
		}
	}
	return -1
}
