package api

import "testing"

// TestWalkFirstIsDeterministic: Go randomizes map iteration, so an unsorted
// depth-first descent picks a different sibling subtree on each run — which made
// album resolution, header parsing and continuation tokens vary per process.
// walkFirst sorts sibling keys, so the same payload must always yield the same
// first match.
func TestWalkFirstIsDeterministic(t *testing.T) {
	// Two sibling keys, each holding a matching renderer. "aShelf" sorts first,
	// so its renderer must win every time regardless of map iteration order.
	payload := map[string]any{
		"zShelf": map[string]any{
			"musicResponsiveListItemRenderer": map[string]any{"pick": "z"},
		},
		"aShelf": map[string]any{
			"musicResponsiveListItemRenderer": map[string]any{"pick": "a"},
		},
	}
	for range 200 {
		got := findRenderer(payload, "musicResponsiveListItemRenderer")
		if str(got["pick"]) != "a" {
			t.Fatalf("findRenderer picked %q, want the lexically first sibling %q", str(got["pick"]), "a")
		}
	}
}

// TestWalkRenderersVisitsEveryMatch: collapsing walkRenderers onto
// walkRenderersMulti must not lose matches, and the visit order must be stable.
func TestWalkRenderersVisitsEveryMatch(t *testing.T) {
	payload := map[string]any{
		"zShelf":   map[string]any{"row": map[string]any{"n": "z"}},
		"aShelf":   map[string]any{"row": map[string]any{"n": "a"}},
		"contents": []any{map[string]any{"row": map[string]any{"n": "1"}}, map[string]any{"row": map[string]any{"n": "2"}}},
	}
	want := "a12z" // aShelf, then contents in slice order, then zShelf
	for range 50 {
		got := ""
		walkRenderers(payload, "row", func(r map[string]any) { got += str(r["n"]) })
		if got != want {
			t.Fatalf("walkRenderers visited %q, want %q", got, want)
		}
	}
}

// TestFindBrowseIDRejectsEmpty: "" doubles as "not found", so a matching
// browseEndpoint with a blank id must not stop the descent.
func TestFindBrowseIDRejectsEmpty(t *testing.T) {
	payload := map[string]any{
		"aShelf": map[string]any{"browseEndpoint": map[string]any{"browseId": ""}},
		"bShelf": map[string]any{"browseEndpoint": map[string]any{"browseId": "MPREb1"}},
	}
	if got := findBrowseID(payload, func(map[string]any) bool { return true }); got != "MPREb1" {
		t.Fatalf("findBrowseID = %q, want MPREb1", got)
	}
}

// TestFindSearchContinuationPrefersContinuationCommand: modern Innertube
// responses carry the next-page token in continuationCommand.token. When both
// that and nextContinuationData.continuation are present, the former must win.
// Without this preference, a legacy response field would override the modern
// one and paging would break on responses that include both.
func TestFindSearchContinuationPrefersContinuationCommand(t *testing.T) {
	payload := map[string]any{
		"contents": []any{
			map[string]any{
				"continuationCommand":  map[string]any{"token": "TOKEN_MODERN"},
				"nextContinuationData": map[string]any{"continuation": "TOKEN_LEGACY"},
			},
		},
	}

	if got := findSearchContinuation(payload); got != "TOKEN_MODERN" {
		t.Fatalf("findSearchContinuation = %q, want TOKEN_MODERN (continuationCommand preferred)", got)
	}
}

// TestFindSearchContinuationFallsBackToLegacy: older API versions omit
// continuationCommand and use nextContinuationData.continuation instead.
// Without this fallback, paging would stop on legacy responses.
func TestFindSearchContinuationFallsBackToLegacy(t *testing.T) {
	payload := map[string]any{
		"contents": []any{
			map[string]any{
				"nextContinuationData": map[string]any{"continuation": "TOKEN_LEGACY"},
			},
		},
	}

	if got := findSearchContinuation(payload); got != "TOKEN_LEGACY" {
		t.Fatalf("findSearchContinuation = %q, want TOKEN_LEGACY", got)
	}
}

// TestFindSearchContinuationEmptyWhenMissing: when neither continuation field
// is present, findSearchContinuation must return "". This is how paging knows
// to stop — a wrong non-empty return would make search loop forever, hanging
// the TUI on a single page of results.
func TestFindSearchContinuationEmptyWhenMissing(t *testing.T) {
	payload := map[string]any{
		"contents": []any{
			map[string]any{
				"someOtherField": "value",
			},
		},
	}

	if got := findSearchContinuation(payload); got != "" {
		t.Fatalf("findSearchContinuation = %q, want empty (no continuation data)", got)
	}
}

// TestFindSearchContinuationEmptyPayload: a nil or empty payload must return
// "", not panic. Some callers pass a partially-built response that may lack
// the contents array entirely.
func TestFindSearchContinuationEmptyPayload(t *testing.T) {
	if got := findSearchContinuation(map[string]any{}); got != "" {
		t.Fatalf("findSearchContinuation = %q, want empty", got)
	}
}
