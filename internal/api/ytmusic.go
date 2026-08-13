package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
)

// maxResponseBytes caps a single Innertube response. Real ones are a few hundred
// KiB; without a cap a broken or hostile reply is read straight into memory.
const maxResponseBytes = 32 << 20

const (
	baseURL       = "https://music.youtube.com/youtubei/v1"
	apiKey        = "AIzaSyC9XL3ZjWddXya6X74dJoCTL-WEYFDNX30"
	clientName    = "WEB_REMIX"
	clientVersion = "1.20230501.01.00"
)

var durationRe = regexp.MustCompile(`^\d+:\d{2}$`)

type Client struct {
	http *http.Client
}

func NewClient() *Client {
	return &Client{http: &http.Client{}}
}

func (c *Client) clientCtx() map[string]any {
	return map[string]any{
		"context": map[string]any{
			"client": map[string]any{
				"clientName":    clientName,
				"clientVersion": clientVersion,
				"hl":            "en",
				"gl":            "US",
			},
		},
	}
}

func (c *Client) Search(ctx context.Context, query string) ([]Track, error) {
	payload := c.clientCtx()
	payload["query"] = query
	payload["params"] = "EgWKAQIIAWoKEAoQAxAEEAkQBQ==" // songs filter

	body, err := c.post(ctx, "search", payload)
	if err != nil {
		return nil, err
	}
	return parseSearchResponse(body)
}

func (c *Client) post(ctx context.Context, endpoint string, payload map[string]any) ([]byte, error) {
	b, err := json.Marshal(payload)
	if err != nil {
		// Dropping this posted an empty body and got back an opaque parse failure.
		return nil, fmt.Errorf("innertube %s: encode payload: %w", endpoint, err)
	}
	url := fmt.Sprintf("%s/%s?key=%s&prettyPrint=false", baseURL, endpoint, apiKey)

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:109.0) Gecko/20100101 Firefox/115.0")
	req.Header.Set("Origin", "https://music.youtube.com")
	req.Header.Set("Referer", "https://music.youtube.com/")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck
	// Without this a 403/429/5xx parsed as empty JSON and rendered as a blank
	// view with no error at all.
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("innertube %s: %s", endpoint, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
}

func parseSearchResponse(data []byte) ([]Track, error) {
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, err
	}

	var tracks []Track
	tabs := digSlice(dig(root, "contents", "tabbedSearchResultsRenderer", "tabs"))
	for _, tab := range tabs {
		contents := digSlice(dig(tab, "tabRenderer", "content", "sectionListRenderer", "contents"))
		for _, section := range contents {
			items := digSlice(dig(section, "musicShelfRenderer", "contents"))
			for _, item := range items {
				r := dig(item, "musicResponsiveListItemRenderer")
				if r == nil {
					continue
				}
				t := extractTrack(r.(map[string]any))
				if t.ID != "" && t.Title != "" {
					tracks = append(tracks, t)
				}
			}
		}
	}
	return tracks, nil
}

func extractTrack(r map[string]any) Track {
	var t Track

	t.ID = str(dig(r, "overlay", "musicItemThumbnailOverlayRenderer", "content",
		"musicPlayButtonRenderer", "playNavigationEndpoint", "watchEndpoint", "videoId"))
	if t.ID == "" {
		t.ID = str(dig(r, "playlistItemData", "videoId"))
	}

	cols := digSlice(dig(r, "flexColumns"))
	if len(cols) > 0 {
		runs := digSlice(dig(cols[0], "musicResponsiveListItemFlexColumnRenderer", "text", "runs"))
		if len(runs) > 0 {
			t.Title = str(dig(runs[0], "text"))
		}
	}
	if len(cols) > 1 {
		runs := digSlice(dig(cols[1], "musicResponsiveListItemFlexColumnRenderer", "text", "runs"))
		texts := extractTexts(runs)
		// texts: [type, artist, album?, year?, duration]
		if len(texts) >= 2 {
			t.Artist = texts[1]
		}
		if len(texts) >= 4 {
			t.Album = texts[2]
		}
		for i := len(texts) - 1; i >= 0; i-- {
			if durationRe.MatchString(texts[i]) {
				t.Duration = texts[i]
				break
			}
		}
	}
	return t
}

func extractTexts(runs []any) []string {
	var out []string
	for _, r := range runs {
		text := strings.TrimSpace(str(dig(r, "text")))
		if text != "" && text != "•" && text != "·" {
			out = append(out, text)
		}
	}
	return out
}

func dig(data any, keys ...string) any {
	cur := data
	for _, k := range keys {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = m[k]
	}
	return cur
}

func digSlice(data any) []any {
	if s, ok := data.([]any); ok {
		return s
	}
	return nil
}

func str(data any) string {
	if s, ok := data.(string); ok {
		return s
	}
	return ""
}
