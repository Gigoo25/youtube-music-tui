package api

import (
	"testing"
)

// TestPickAlbumCandidateExactMatch: exact (case-insensitive) match beats every
// other tier. Without this priority, a candidate with a substring match (e.g.
// "Bohemian Rhapsody" matching want "Rhapsody") would win over the actual
// album "Bohemian Rhapsody (Single)".
func TestPickAlbumCandidateExactMatch(t *testing.T) {
	cands := []albumCandidate{
		{id: "album-substring", title: "Bohemian Rhapsody (Single)"},
		{id: "album-exact", title: "BOHEMIAN RHAPSODY"},
	}

	got, ok := pickAlbumCandidate(cands, "bohemian rhapsody")
	if !ok {
		t.Fatal("pickAlbumCandidate must find a match")
	}
	if got != "album-exact" {
		t.Fatalf("got id %q, want album-exact (exact match beats substring)", got)
	}
}

// TestPickAlbumCandidatePrefixBeatsSubstring: a candidate that extends the
// wanted name ("X (Deluxe Edition)" for want "X") must beat a candidate that
// only contains the wanted name. This is how "Dark Side of the Moon" matches
// "Dark Side of the Moon (Remastered)" rather than some random album that
// happens to contain those words.
func TestPickAlbumCandidatePrefixBeatsSubstring(t *testing.T) {
	cands := []albumCandidate{
		// This candidate contains the wanted string but does NOT have it as a
		// prefix — it starts with "On the" instead.
		{id: "cand-substring", title: "On the Moon (Remastered)"},
		{id: "cand-prefix", title: "Dark Side of the Moon (Remastered)"},
	}

	got, ok := pickAlbumCandidate(cands, "Dark Side of the Moon")
	if !ok {
		t.Fatal("pickAlbumCandidate must find a match")
	}
	if got != "cand-prefix" {
		t.Fatalf("got id %q, want cand-prefix (prefix match beats substring)", got)
	}
}

// TestPickAlbumCandidateSubstringBeatsLastResort: a candidate that contains
// the wanted name must beat a candidate that the wanted name extends. The
// last-resort tier exists for edge cases like "Artist — Album" glued together;
// it must not win over a genuine substring match.
func TestPickAlbumCandidateSubstringBeatsLastResort(t *testing.T) {
	cands := []albumCandidate{
		{id: "cand-last-resort", title: "Something Else"},
		{id: "cand-substring", title: "Pink Floyd — The Wall"},
	}

	got, ok := pickAlbumCandidate(cands, "Pink")
	if !ok {
		t.Fatal("pickAlbumCandidate must find a match")
	}
	if got != "cand-substring" {
		t.Fatalf("got id %q, want cand-substring (substring beats last resort)", got)
	}
}

// TestPickAlbumCandidateEmptyWant: an empty want string must return ok=false.
// This prevents the caller from accidentally matching every album when the
// user types nothing (e.g. the filter line is empty).
func TestPickAlbumCandidateEmptyWant(t *testing.T) {
	cands := []albumCandidate{
		{id: "a1", title: "Some Album"},
	}
	if _, ok := pickAlbumCandidate(cands, ""); ok {
		t.Fatal("pickAlbumCandidate must return ok=false for empty want")
	}
}

// TestPickAlbumCandidateEmptyCandidates: an empty candidate list must return
// ok=false. Without this, a network error that leaves the candidate list
// empty would still return a match (the zero value of id is "").
func TestPickAlbumCandidateEmptyCandidates(t *testing.T) {
	if _, ok := pickAlbumCandidate(nil, "Some Album"); ok {
		t.Fatal("pickAlbumCandidate must return ok=false for nil candidates")
	}
	if _, ok := pickAlbumCandidate([]albumCandidate{}, "Some Album"); ok {
		t.Fatal("pickAlbumCandidate must return ok=false for empty candidates")
	}
}

// TestPickAlbumCandidateNothingMatches: when no tier matches, ok must be false.
// The caller falls back to a manual selection or a broader search on this
// signal — a false match would silently pick the wrong album.
func TestPickAlbumCandidateNothingMatches(t *testing.T) {
	cands := []albumCandidate{
		{id: "a1", title: "Completely Different Album"},
	}
	if _, ok := pickAlbumCandidate(cands, "Neverheardofthis"); ok {
		t.Fatal("pickAlbumCandidate must return ok=false when nothing matches")
	}
}

// TestPickAlbumCandidateEarlierEntryWins: when two candidates land in the same
// tier, the earlier slice entry must win. This is the end-to-end guarantee
// walkFirst's sorted descent exists to provide — without it, the album picker
// would pick non-deterministically between equally-good candidates, and the
// user's first result would change between runs.
func TestPickAlbumCandidateEarlierEntryWins(t *testing.T) {
	cands := []albumCandidate{
		{id: "first", title: "Abbey Road"},
		{id: "second", title: "Abbey Road (Remaster)"},
	}

	// Loop 50× to catch any non-determinism.
	for i := 0; i < 50; i++ {
		got, ok := pickAlbumCandidate(cands, "abbey road")
		if !ok {
			t.Fatalf("iteration %d: pickAlbumCandidate must find a match", i)
		}
		if got != "first" {
			t.Fatalf("iteration %d: got id %q, want 'first' (earlier entry must win)", i, got)
		}
	}
}

// TestPickAlbumCandidateWhitespaceTolerated: leading/trailing whitespace in
// both want and candidate titles must be ignored. YouTube's API sometimes
// returns titles with surrounding spaces.
func TestPickAlbumCandidateWhitespaceTolerated(t *testing.T) {
	cands := []albumCandidate{
		{id: "a1", title: "  Abbey Road  "},
	}

	got, ok := pickAlbumCandidate(cands, "  abbey road  ")
	if !ok {
		t.Fatal("pickAlbumCandidate must match despite surrounding whitespace")
	}
	if got != "a1" {
		t.Fatalf("got id %q, want a1", got)
	}
}

// TestPickAlbumCandidateCaseInsensitive: the match must be case-insensitive.
// YouTube returns titles in mixed case; the user's want string may be any case.
func TestPickAlbumCandidateCaseInsensitive(t *testing.T) {
	cands := []albumCandidate{
		{id: "a1", title: "Abbey Road"},
	}

	cases := []string{"ABBEY ROAD", "abbey road", "AbBeY rOaD", "abbey road"}
	for _, want := range cases {
		if _, ok := pickAlbumCandidate(cands, want); !ok {
			t.Fatalf("pickAlbumCandidate must match %q case-insensitively", want)
		}
	}
}
