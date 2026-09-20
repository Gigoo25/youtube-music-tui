package player

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func closeTestResource(t *testing.T, resource interface{ Close() error }) {
	t.Helper()
	if err := resource.Close(); err != nil {
		t.Errorf("close test resource: %v", err)
	}
}

func TestSocketPathForUsesRuntimeDirectory(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", dir)
	got, cleanup := socketPathFor()
	if filepath.Dir(got) != dir || !strings.HasSuffix(got, ".sock") {
		t.Fatalf("socketPathFor = %q", got)
	}
	if cleanup != "" {
		t.Fatalf("cleanup dir = %q, want empty when XDG_RUNTIME_DIR is set", cleanup)
	}
}

// TestSocketPathForFallbackIsPrivate: with no XDG_RUNTIME_DIR the socket must
// not land directly in the shared temp dir — the mpv IPC socket accepts
// arbitrary commands, so it lives in a private 0700 directory instead.
func TestSocketPathForFallbackIsPrivate(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "")
	got, cleanup := socketPathFor()
	if cleanup == "" {
		t.Fatalf("socketPathFor fallback = %q, want a private temp dir", got)
	}
	defer os.RemoveAll(cleanup) //nolint:errcheck
	if filepath.Dir(got) != cleanup {
		t.Fatalf("socket %q is not inside its private dir %q", got, cleanup)
	}
	fi, err := os.Stat(cleanup)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o700 {
		t.Fatalf("private dir mode = %o, want 0700", perm)
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
	defer closeTestResource(t, ln)
	accepted := make(chan struct{})
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			t.Errorf("accept test connection: %v", err)
			close(accepted)
			return
		}
		closeTestResource(t, conn)
		close(accepted)
	}()
	conn, err := dialWithRetry(path, 3, time.Millisecond, make(chan struct{}))
	if err != nil {
		t.Fatal(err)
	}
	closeTestResource(t, conn)
	<-accepted

	abort := make(chan struct{})
	close(abort)
	if _, err := dialWithRetry(filepath.Join(t.TempDir(), "missing.sock"), 3, time.Millisecond, abort); err == nil {
		t.Fatal("dialWithRetry on aborted connection returned nil error")
	}
}

func TestWriteLoopWritesQueuedCommand(t *testing.T) {
	p := scanPlayer()
	left, right := net.Pipe()
	defer closeTestResource(t, left)
	defer closeTestResource(t, right)
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
	defer closeTestResource(t, left)
	defer closeTestResource(t, right)
	p.adoptConn(left)
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.conn != nil {
		t.Fatal("adoptConn installed connection after Close")
	}
}
