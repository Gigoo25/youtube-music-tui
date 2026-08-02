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
