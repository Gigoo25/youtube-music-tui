package player

import (
	"testing"
	"time"
)

// TestCacheRoundTrip: cachePut/cacheGet is the warm path for every track change.
// If a freshly cached URL vanishes on the very next read, auto-advance stalls
// and the TUI shows a spinner for the full yt-dlp resolve latency on every skip.
func TestCacheRoundTrip(t *testing.T) {
	p := scanPlayer()
	defer p.baseCancel()

	p.cachePut("vid1", "http://stream.example/a")

	url, ok := p.cacheGet("vid1")
	if !ok {
		t.Fatal("cacheGet should hit for a freshly put entry")
	}
	if url != "http://stream.example/a" {
		t.Fatalf("url = %q, want %q", url, "http://stream.example/a")
	}
}

// TestCacheMissOnUnknownId: an id never cached must return ok=false. Load()
// treats a miss as "go resolve via yt-dlp"; a false miss would skip the
// resolve and hand an empty URL to mpv, which would fail with a confusing
// error the user can't act on.
func TestCacheMissOnUnknownId(t *testing.T) {
	p := scanPlayer()
	defer p.baseCancel()

	if _, ok := p.cacheGet("never-put"); ok {
		t.Fatal("cacheGet must miss for an id that was never cached")
	}
}

// TestCacheTTLExpiry: cached URLs carry an `expire` query param that yt-dlp
// stamps with a unix timestamp hours in the future. urlTTL keeps us well
// inside that window, but once the entry ages past urlTTL it must be treated
// as stale — a stale googlevideo URL that passed its `expire` param would
// decode to a 403, and the player would replay the same dead URL forever.
//
// We backdate the entry's `at` field instead of sleeping: the test must be
// fast and deterministic, and time.Sleep would make it flaky.
func TestCacheTTLExpiry(t *testing.T) {
	p := scanPlayer()
	defer p.baseCancel()

	p.cachePut("vid", "http://stream.example/x")

	// Reach into the cache and backdate the entry past urlTTL.
	p.mu.Lock()
	c := p.urlCache["vid"]
	c.at = time.Now().Add(-2 * urlTTL)
	p.urlCache["vid"] = c
	p.mu.Unlock()

	if _, ok := p.cacheGet("vid"); ok {
		t.Fatal("cacheGet must miss for a backdated entry (TTL expired)")
	}
}

// TestCacheFreshEntrySurvivesTTLCheck: an entry that has not exceeded urlTTL
// must still be returned. This is the inverse of the expiry test — without it
// a correctly-warmed cache would appear broken.
func TestCacheFreshEntrySurvivesTTLCheck(t *testing.T) {
	p := scanPlayer()
	defer p.baseCancel()

	p.cachePut("vid", "http://stream.example/fresh")

	if _, ok := p.cacheGet("vid"); !ok {
		t.Fatal("freshly put entry must survive the TTL check")
	}
}

// TestCacheEvictionRemovesStale: cachePut sweeps expired entries on every call.
// After a long session with many tracks, stale entries must not accumulate —
// they waste memory and pollute the map on read (each read does a lookup, and
// a map full of stale entries slows that down). The sweep must keep fresh
// entries intact while removing the expired ones.
func TestCacheEvictionRemovesStale(t *testing.T) {
	p := scanPlayer()
	defer p.baseCancel()

	// Warm a few entries.
	p.cachePut("a", "http://a.example")
	p.cachePut("b", "http://b.example")
	p.cachePut("c", "http://c.example")

	// Backdate two of them.
	p.mu.Lock()
	ca := p.urlCache["a"]
	ca.at = time.Now().Add(-2 * urlTTL)
	p.urlCache["a"] = ca
	cc := p.urlCache["c"]
	cc.at = time.Now().Add(-2 * urlTTL)
	p.urlCache["c"] = cc
	p.mu.Unlock()

	// Trigger the sweep by putting a new entry.
	p.cachePut("d", "http://d.example")

	// "b" is fresh — must survive.
	if _, ok := p.cacheGet("b"); !ok {
		t.Fatal("fresh entry 'b' must survive eviction sweep")
	}
	// "d" is fresh — must survive.
	if _, ok := p.cacheGet("d"); !ok {
		t.Fatal("fresh entry 'd' must survive eviction sweep")
	}
	// "a" and "c" are stale — must be gone.
	if _, ok := p.cacheGet("a"); ok {
		t.Fatal("stale entry 'a' must be evicted")
	}
	if _, ok := p.cacheGet("c"); ok {
		t.Fatal("stale entry 'c' must be evicted")
	}
}

// TestCacheOverwritePreservesFreshness: re-caching the same id with a new URL
// must refresh the `at` timestamp. Without this, a URL rotation (googlevideo
// expire params roll over) would never take effect because the original
// timestamp keeps the entry "fresh" forever.
func TestCacheOverwritePreservesFreshness(t *testing.T) {
	p := scanPlayer()
	defer p.baseCancel()

	p.cachePut("vid", "http://old.example")
	p.mu.Lock()
	cv := p.urlCache["vid"]
	cv.at = time.Now().Add(-2 * urlTTL)
	p.urlCache["vid"] = cv
	p.mu.Unlock()

	// Overwrite: this triggers the sweep (which would normally drop the entry)
	// and then writes a fresh entry.
	p.cachePut("vid", "http://new.example")

	url, ok := p.cacheGet("vid")
	if !ok {
		t.Fatal("overwritten entry must be present")
	}
	if url != "http://new.example" {
		t.Fatalf("url = %q, want %q", url, "http://new.example")
	}
}

// TestCacheNilMapHandled: a Player with a nil urlCache must not panic on
// cachePut. New() never leaves urlCache nil, but tests that build a Player
// by hand (like scanPlayer) start with a nil map — cachePut must be safe to
// call on such a player.
func TestCacheNilMapHandled(t *testing.T) {
	p := scanPlayer()
	defer p.baseCancel()

	// urlCache is nil — cachePut must initialize it.
	p.cachePut("vid", "http://stream")

	if _, ok := p.cacheGet("vid"); !ok {
		t.Fatal("cachePut on nil map must initialize the map")
	}
}
