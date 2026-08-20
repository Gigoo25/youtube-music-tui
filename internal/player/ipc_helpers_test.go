package player

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSocketPathForUsesRuntimeDirectory(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", dir)
	if got := socketPathFor(); filepath.Dir(got) != dir || !strings.HasSuffix(got, ".sock") {
		t.Fatalf("socketPathFor = %q", got)
	}
	t.Setenv("XDG_RUNTIME_DIR", "")
	if got := socketPathFor(); filepath.Dir(got) != os.TempDir() {
		t.Fatalf("socketPathFor fallback = %q", got)
	}
}

func TestYTDLPReason(t *testing.T) {
	if got := ytdlpReason([]byte("first\nERROR: unavailable\n")); got != "ERROR: unavailable" {
		t.Fatalf("ytdlpReason = %q", got)
	}
	long := strings.Repeat("x", 250)
	if got := ytdlpReason([]byte(long)); len(got) != 200 {
		t.Fatalf("ytdlpReason length = %d, want 200", len(got))
	}
}

func TestValidVideoIDBoundaries(t *testing.T) {
	for _, tc := range []struct {
		id string
		ok bool
	}{
		{"dQw4w9WgXcQ", true},
		{"", false},
		{"bad/id", false},
		{strings.Repeat("a", 65), false},
	} {
		if got := validVideoID(tc.id); got != tc.ok {
			t.Errorf("validVideoID(%q) = %v, want %v", tc.id, got, tc.ok)
		}
	}
}

func TestDialWithRetryConnectsAndAborts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "player.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		conn, _ := ln.Accept()
		if conn != nil {
			conn.Close()
		}
	}()
	conn, err := dialWithRetry(path, 3, time.Millisecond, make(chan struct{}))
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()

	abort := make(chan struct{})
	close(abort)
	if _, err := dialWithRetry(filepath.Join(t.TempDir(), "missing.sock"), 3, time.Millisecond, abort); err == nil {
		t.Fatal("dialWithRetry on aborted connection returned nil error")
	}
}

func TestWriteLoopWritesQueuedCommand(t *testing.T) {
	p := scanPlayer()
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	p.conn = left
	go p.writeLoop()
	p.sendCh <- []byte("command\n")
	buf := make([]byte, 8)
	if _, err := right.Read(buf); err != nil || string(buf) != "command\n" {
		t.Fatalf("writeLoop wrote %q, err=%v", buf, err)
	}
}

func TestAdoptConnAfterCloseDoesNotInstallConnection(t *testing.T) {
	p := scanPlayer()
	close(p.closed)
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	p.adoptConn(left)
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.conn != nil {
		t.Fatal("adoptConn installed connection after Close")
	}
}
