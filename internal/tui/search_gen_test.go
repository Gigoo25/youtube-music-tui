package tui

import (
	"testing"

	"github.com/Gigoo25/youtube-music-tui/internal/api"
)

// TestEscClearingSearchInvalidatesInFlight guards F16: esc empties the results
// list, so any search response still on the wire must be dropped. Clearing
// without bumping the epoch lets a late searchDoneMsg refill the list the user
// just emptied.
func TestEscClearingSearchInvalidatesInFlight(t *testing.T) {
	m := newTestModel()
	m.activeView = viewSearch
	m.focus = focusPanel
	m.searchResults = []api.Track{{ID: "a", Title: "one"}, {ID: "b", Title: "two"}}

	gen := m.searchGen
	m.Update(key("esc"))

	if m.searchResults != nil {
		t.Fatalf("esc left %d search results, want nil", len(m.searchResults))
	}
	if m.searchGen == gen {
		t.Fatalf("esc did not bump searchGen (still %d): in-flight results can refill the list", gen)
	}
}
