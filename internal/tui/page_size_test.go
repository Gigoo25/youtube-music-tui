package tui

import (
	"testing"
)

// TestPageSizeBaseCase: pageSize must subtract the list header row from the
// viewport height. Without this, the last visible row would be partially
// clipped — the TUI would render 20 rows in a 20-row viewport, but the header
// eats one, so only 19 data rows fit.
func TestPageSizeBaseCase(t *testing.T) {
	m := newTestModel()
	m.viewportH = 20

	if got := m.pageSize(); got != 19 {
		t.Fatalf("pageSize() = %d, want 19 (viewportH - 1 header)", got)
	}
}

// TestPageSizeFilterLine: when the filter line is showing (m.filter != ""),
// pageSize must subtract one more row. The filter line renders above the list
// and steals a row from the data area.
func TestPageSizeFilterLine(t *testing.T) {
	m := newTestModel()
	m.viewportH = 20
	m.filter = "x"

	if got := m.pageSize(); got != 18 {
		t.Fatalf("pageSize() with filter = %d, want 18", got)
	}
}

// TestPageSizeFilteringMode: m.filtering = true must also reduce pageSize by
// one, since the filter input line is visible in this mode.
func TestPageSizeFilteringMode(t *testing.T) {
	m := newTestModel()
	m.viewportH = 20
	m.filtering = true

	if got := m.pageSize(); got != 18 {
		t.Fatalf("pageSize() with filtering = %d, want 18", got)
	}
}

// TestPageSizeNamingLine: m.naming = true must reduce pageSize by one for the
// same reason as the filter line.
func TestPageSizeNamingLine(t *testing.T) {
	m := newTestModel()
	m.viewportH = 20
	m.naming = true

	if got := m.pageSize(); got != 18 {
		t.Fatalf("pageSize() with naming = %d, want 18", got)
	}
}

// TestPageSizePreRenderFallback: when viewportH is 0 (pre-first-render),
// pageSize must return the minimum of 10. Without this, half-page scroll
// commands before the first render would compute a page size of 0 or negative,
// making j/k/ctrl+d/ctrl+u no-ops or causing integer underflow in vimMove.
func TestPageSizePreRenderFallback(t *testing.T) {
	m := newTestModel()
	m.viewportH = 0

	if got := m.pageSize(); got != 10 {
		t.Fatalf("pageSize() with viewportH=0 = %d, want 10 (minimum)", got)
	}
}

// TestPageSizeCombinedChrome: when both naming and filter are showing, pageSize
// must subtract both. The two lines share the same code path (the || check)
// and each steals one row.
func TestPageSizeCombinedChrome(t *testing.T) {
	m := newTestModel()
	m.viewportH = 20
	m.naming = true
	m.filter = "x"

	if got := m.pageSize(); got != 18 {
		t.Fatalf("pageSize() with naming+filter = %d, want 18 (each steals one row)", got)
	}
}
