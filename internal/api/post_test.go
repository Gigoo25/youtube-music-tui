package api

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

// stubTransport answers every request with a fixed status and body.
type stubTransport struct {
	code int
	body string
}

func (s stubTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: s.code,
		Status:     http.StatusText(s.code),
		Body:       io.NopCloser(strings.NewReader(s.body)),
		Header:     make(http.Header),
	}, nil
}

// TestPostRejectsNon2xx guards A1: Innertube answers a rate-limit, a region
// block, or an outage with 403/429/5xx and an HTML body. Handing that to the JSON
// parser produced an empty result set and no error, so Home and Search rendered
// blank with nothing on screen to explain why.
func TestPostRejectsNon2xx(t *testing.T) {
	for _, code := range []int{403, 429, 500, 503} {
		c := NewClient()
		c.http.Transport = stubTransport{code: code, body: "<html>denied</html>"}

		body, err := c.post(context.Background(), "search", map[string]any{})
		if err == nil {
			t.Fatalf("status %d: post returned nil error and %d bytes, want an error", code, len(body))
		}
		if !strings.Contains(err.Error(), "search") {
			t.Errorf("status %d: error %q does not name the endpoint", code, err)
		}
	}
}

// TestPostAcceptsSuccess is the counterpart: a 2xx body must still come back
// untouched, so the status check above cannot reject healthy responses.
func TestPostAcceptsSuccess(t *testing.T) {
	c := NewClient()
	c.http.Transport = stubTransport{code: 200, body: `{"ok":true}`}

	body, err := c.post(context.Background(), "search", map[string]any{})
	if err != nil {
		t.Fatal("post on 200:", err)
	}
	if string(body) != `{"ok":true}` {
		t.Fatalf("post returned %q, want the response body verbatim", body)
	}
}

// TestExtractPanelTrackFallsBackToShortByline guards A6: Innertube sends
// "longBylineText": {"runs": []} for some radio entries, and json.Unmarshal turns
// that into a non-nil empty slice. The old nil check therefore never fired and the
// shortBylineText fallback was dead code, leaving those tracks with no artist.
func TestExtractPanelTrackFallsBackToShortByline(t *testing.T) {
	r := map[string]any{
		"videoId": "vid1",
		"title":   map[string]any{"runs": []any{map[string]any{"text": "Song"}}},
		// Present but empty — the shape that defeated the nil check.
		"longBylineText":  map[string]any{"runs": []any{}},
		"shortBylineText": map[string]any{"runs": []any{map[string]any{"text": "Real Artist"}}},
	}
	if got := extractPanelTrack(r).Artist; got != "Real Artist" {
		t.Fatalf("Artist = %q, want %q from the shortBylineText fallback", got, "Real Artist")
	}
}
