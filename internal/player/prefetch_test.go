package player

import (
	"context"
	"testing"
)

// testPlayer is a Player with only the fields the resolve bookkeeping needs.
// claimResolve derives each resolve ctx from baseCtx, so it must be non-nil.
func testPlayer() *Player {
	p := &Player{}
	p.baseCtx, p.baseCancel = context.WithCancel(context.Background())
	return p
}

// TestClaimResolveDedup: a videoID can only be claimed once until released, so
// concurrent Prefetch calls never spawn duplicate yt-dlp resolves for it.
func TestClaimResolveDedup(t *testing.T) {
	p := testPlayer()
	defer p.baseCancel()

	if _, ok := p.claimResolve("vid"); !ok {
		t.Fatal("first claim should succeed")
	}
	if _, ok := p.claimResolve("vid"); ok {
		t.Fatal("second claim while in-flight should be denied (dedup)")
	}
	if _, ok := p.claimResolve("other"); !ok {
		t.Fatal("a different id should claim independently")
	}
	p.releaseResolve("vid")
	if _, ok := p.claimResolve("vid"); !ok {
		t.Fatal("after release the id should be claimable again")
	}
}

// TestClaimResolveSkipsCached: an already-cached URL needs no fresh resolve.
func TestClaimResolveSkipsCached(t *testing.T) {
	p := testPlayer()
	defer p.baseCancel()

	p.cachePut("vid", "http://stream")
	if _, ok := p.claimResolve("vid"); ok {
		t.Fatal("a cached id should not be claimed for resolving")
	}
}

// TestClaimResolveCapsConcurrency: skipping through a queue asks for a fresh
// prefetch batch per track. Without a cap those pile up as concurrent yt-dlp
// processes, so the oldest resolve must be evicted (and cancelled) to make room.
func TestClaimResolveCapsConcurrency(t *testing.T) {
	p := testPlayer()
	defer p.baseCancel()

	ctxs := make([]context.Context, 0, maxInflightResolves)
	for i := range maxInflightResolves {
		ctx, ok := p.claimResolve(string(rune('a' + i)))
		if !ok {
			t.Fatalf("claim %d should succeed below the cap", i)
		}
		ctxs = append(ctxs, ctx)
	}
	if len(p.inflight) != maxInflightResolves {
		t.Fatalf("in-flight = %d, want the cap %d", len(p.inflight), maxInflightResolves)
	}

	// One past the cap: accepted, and the oldest ("a") is evicted.
	if _, ok := p.claimResolve("z"); !ok {
		t.Fatal("a claim at the cap should evict, not be refused")
	}
	if len(p.inflight) != maxInflightResolves {
		t.Fatalf("in-flight = %d after eviction, want %d", len(p.inflight), maxInflightResolves)
	}
	if _, still := p.inflight["a"]; still {
		t.Fatal("oldest claim should have been evicted")
	}
	if ctxs[0].Err() == nil {
		t.Fatal("evicted resolve's context should be cancelled so its yt-dlp exits")
	}
	if ctxs[1].Err() != nil {
		t.Fatal("a surviving resolve must not be cancelled by the eviction")
	}

	// The evicted id is claimable again — a later prefetch pass can re-warm it.
	if _, ok := p.claimResolve("a"); !ok {
		t.Fatal("an evicted id should be claimable again")
	}
}

// TestReleaseResolveCancels: the resolve ctx carries a loadTimeout timer, so
// releasing must cancel it rather than leave it to fire.
func TestReleaseResolveCancels(t *testing.T) {
	p := testPlayer()
	defer p.baseCancel()

	ctx, _ := p.claimResolve("vid")
	p.releaseResolve("vid")
	if ctx.Err() == nil {
		t.Fatal("released resolve's context should be cancelled")
	}
	if len(p.inflightFIFO) != 0 {
		t.Fatalf("eviction queue = %v, want empty after release", p.inflightFIFO)
	}
}
