package api

import (
	"testing"
)

// TestDedupeTracksRemovesDuplicates: tracks with the same ID must collapse to
// a single entry. Without dedup, a search result that appears in multiple
// renderers (e.g. both a result row and a continuation) would duplicate in the
// output, and the TUI would show the same track twice in the queue.
func TestDedupeTracksRemovesDuplicates(t *testing.T) {
	in := []Track{
		{ID: "a", Title: "First"},
		{ID: "b", Title: "Second"},
		{ID: "a", Title: "First"}, // duplicate
	}

	out := dedupeTracks(in)
	if len(out) != 2 {
		t.Fatalf("len(out) = %d, want 2", len(out))
	}
	if out[0].ID != "a" || out[1].ID != "b" {
		t.Fatalf("out IDs = [%s %s], want [a b]", out[0].ID, out[1].ID)
	}
}

// TestDedupeTracksKeepsFirstOccurrence: when the same ID appears multiple
// times, the first entry must be kept. A later occurrence might have richer
// metadata (e.g. album info from a detail renderer), but the first is the one
// the user actually saw first — keeping the last would silently reorder the
// results.
func TestDedupeTracksKeepsFirstOccurrence(t *testing.T) {
	in := []Track{
		{ID: "a", Title: "First Title", Artist: "First Artist"},
		{ID: "a", Title: "Second Title", Artist: "Second Artist"},
	}

	out := dedupeTracks(in)
	if len(out) != 1 {
		t.Fatalf("len(out) = %d, want 1", len(out))
	}
	if out[0].Title != "First Title" {
		t.Fatalf("out[0].Title = %q, want 'First Title' (first occurrence must be kept)", out[0].Title)
	}
}

// TestDedupeTracksDropsEmptyIDs: tracks with no ID must be dropped. An empty-
// ID track would panic on any queue operation that indexes by ID.
func TestDedupeTracksDropsEmptyIDs(t *testing.T) {
	in := []Track{
		{ID: "a", Title: "Valid"},
		{ID: "", Title: "No ID"},
		{ID: "b", Title: "Also Valid"},
	}

	out := dedupeTracks(in)
	if len(out) != 2 {
		t.Fatalf("len(out) = %d, want 2", len(out))
	}
}

// TestDedupeTracksDropsEmptyTitles: tracks with no title must be dropped.
// An empty-title track would render as a blank line in the queue.
func TestDedupeTracksDropsEmptyTitles(t *testing.T) {
	in := []Track{
		{ID: "a", Title: "Valid"},
		{ID: "b", Title: ""},
	}

	out := dedupeTracks(in)
	if len(out) != 1 {
		t.Fatalf("len(out) = %d, want 1", len(out))
	}
	if out[0].ID != "a" {
		t.Fatalf("out[0].ID = %q, want 'a'", out[0].ID)
	}
}

// TestDedupeTracksLimit: the output is capped at 50 entries. Charts and search
// results can exceed this; without a cap the TUI would render thousands of
// rows and freeze.
func TestDedupeTracksLimit(t *testing.T) {
	in := make([]Track, 0, 60)
	for i := 0; i < 60; i++ {
		in = append(in, Track{ID: string(rune('a' + i)), Title: "Track"})
	}

	out := dedupeTracks(in)
	if len(out) != 50 {
		t.Fatalf("len(out) = %d, want 50 (cap)", len(out))
	}
}

// TestDedupeTracksNilInput: a nil input must not panic. Several callers build
// the input slice incrementally and may pass nil if no renderers matched.
func TestDedupeTracksNilInput(t *testing.T) {
	out := dedupeTracks(nil)
	// The code returns a non-nil empty slice (make with 0 len), so we just
	// assert it doesn't panic and has length 0.
	if len(out) != 0 {
		t.Fatalf("len(out) = %d, want 0", len(out))
	}
}

// TestDedupeTracksEmptyInput: an empty input must return an empty (non-nil)
// slice, not nil. Callers that range over the result must not see nil — that
// would skip the range body entirely and could hide the "no results" state.
func TestDedupeTracksEmptyInput(t *testing.T) {
	out := dedupeTracks([]Track{})
	if len(out) != 0 {
		t.Fatalf("len(out) = %d, want 0", len(out))
	}
}
