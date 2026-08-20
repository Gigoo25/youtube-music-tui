package player

import (
	"encoding/json"
	"testing"
)

func TestPlaybackCommands(t *testing.T) {
	p := scanPlayer()
	defer p.baseCancel()

	p.PlayPause()
	p.Play()
	p.Pause()
	p.ToggleMute()
	p.Seek(5)
	p.SeekAbs(12)
	p.SetTitle("Song")

	want := [][]any{
		{"cycle", "pause"},
		{"set_property", "pause", false},
		{"set_property", "pause", true},
		{"cycle", "mute"},
		{"seek", float64(5), "relative"},
		{"seek", float64(12), "absolute"},
		{"set_property", "force-media-title", "Song"},
	}
	for i, expected := range want {
		select {
		case raw := <-p.sendCh:
			var got ipcCmd
			if err := json.Unmarshal(raw, &got); err != nil {
				t.Fatalf("command %d: decode: %v", i, err)
			}
			if len(got.Command) != len(expected) {
				t.Fatalf("command %d = %v, want %v", i, got.Command, expected)
			}
			for j := range expected {
				if got.Command[j] != expected[j] {
					t.Fatalf("command %d = %v, want %v", i, got.Command, expected)
				}
			}
		default:
			t.Fatalf("missing command %d, want %v", i, expected)
		}
	}
}

func TestSetTitleIgnoresEmptyTitle(t *testing.T) {
	p := scanPlayer()
	defer p.baseCancel()
	p.SetTitle("")
	select {
	case got := <-p.sendCh:
		t.Fatalf("empty title queued command %s", got)
	default:
	}
}
