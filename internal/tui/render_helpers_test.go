package tui

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestRenderHelpers(t *testing.T) {
	for _, tc := range []struct {
		name, got, want string
	}{
		{"duration", fmtDur(61.9), "1:01"},
		{"negative duration", fmtDur(-1), "0:00"},
		{"truncate zero", truncate("abc", 0), ""},
		{"truncate one", truncate("abc", 1), "…"},
		{"truncate fits", truncate("abc", 3), "abc"},
		{"pad right", padRight("x", 3), "x  "},
		{"pad height", padToHeight("a", 2), "a\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Fatalf("got %q, want %q", tc.got, tc.want)
			}
		})
	}
}

func TestRenderHelpersClampWidths(t *testing.T) {
	if got := padRight("abcdef", 3); got != "abc" {
		t.Fatalf("padRight crop = %q", got)
	}
	if got := padToHeight("a\nb\nc", 2); got != "a\nb" {
		t.Fatalf("padToHeight crop = %q", got)
	}
	if got := rowLR("long label", "right", 8); lipgloss.Width(got) != 8 {
		t.Fatalf("rowLR width = %d, want 8 (%q)", lipgloss.Width(got), got)
	}
	if got := listRow("1. ", "Song", "1:00", true, true, 20); lipgloss.Width(got) != 20 {
		t.Fatalf("selected listRow width = %d, want 20", lipgloss.Width(got))
	}
	if got := listRow("1. ", "Song", "1:00", false, false, 20); got == listRow("1. ", "Song", "1:00", true, false, 20) {
		t.Fatal("selected and unselected listRow should differ")
	}
}
