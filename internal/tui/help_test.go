package tui

import "testing"

// TestHelpFromSidebarReturnsToPreviousView: Help is a sidebar tab too, so "?"
// must close it back to the view the user came from instead of falling back to
// Home (activateView remembers it).
func TestHelpFromSidebarReturnsToPreviousView(t *testing.T) {
	m := newTestModel()
	m.activeView = viewQueue
	m.focus = focusSidebar
	m.navCursor = navIndexOf(viewHelp)

	press(m, "enter")
	if m.activeView != viewHelp {
		t.Fatalf("sidebar enter on Help landed on %v, want viewHelp", m.activeView)
	}
	press(m, "?")
	if m.activeView != viewQueue {
		t.Fatalf("? on Help landed on %v, want the previous view (viewQueue)", m.activeView)
	}
}
