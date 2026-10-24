package player

import (
	"context"
	"slices"
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

	_, gen1, _, ok := p.claimResolve("vid")
	if !ok {
		t.Fatal("first claim should succeed")
	}
	if _, _, _, ok := p.claimResolve("vid"); ok {
		t.Fatal("second claim while in-flight should be denied (dedup)")
	}
	_, _, _, ok = p.claimResolve("other")
	if !ok {
		t.Fatal("a different id should claim independently")
	}
	p.releaseResolve("vid", gen1)
	if _, _, _, ok := p.claimResolve("vid"); !ok {
		t.Fatal("after release the id should be claimable again")
	}
}

// TestClaimResolveSkipsCached: an already-cached URL needs no fresh resolve.
func TestClaimResolveSkipsCached(t *testing.T) {
	p := testPlayer()
	defer p.baseCancel()

	p.cachePut("vid", "http://stream")
	if _, _, _, ok := p.claimResolve("vid"); ok {
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
		ctx, _, _, ok := p.claimResolve(string(rune('a' + i)))
		if !ok {
			t.Fatalf("claim %d should succeed below the cap", i)
		}
		ctxs = append(ctxs, ctx)
	}
	if len(p.inflight) != maxInflightResolves {
		t.Fatalf("in-flight = %d, want the cap %d", len(p.inflight), maxInflightResolves)
	}

	// One past the cap: accepted, and the oldest ("a") is evicted.
	if _, _, _, ok := p.claimResolve("z"); !ok {
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
	if _, _, _, ok := p.claimResolve("a"); !ok {
		t.Fatal("an evicted id should be claimable again")
	}
}

// TestReleaseResolveCancels: the resolve ctx carries a loadTimeout timer, so
// releasing must cancel it rather than leave it to fire.
func TestReleaseResolveCancels(t *testing.T) {
	p := testPlayer()
	defer p.baseCancel()

	ctx, gen, _, _ := p.claimResolve("vid")
	p.releaseResolve("vid", gen)
	if ctx.Err() == nil {
		t.Fatal("released resolve's context should be cancelled")
	}
	if len(p.inflightFIFO) != 0 {
		t.Fatalf("eviction queue = %v, want empty after release", p.inflightFIFO)
	}
}

// TestPrefetchDeferredWhileLoadResolves: prefetches requested while a Load is
// resolving its URL wait for it (newest kept, deduped) instead of competing
// with it, and are handed back once that load finishes — but not by a load a
// newer one superseded.
func TestPrefetchDeferredWhileLoadResolves(t *testing.T) {
	p := testPlayer()
	defer p.baseCancel()

	p.resolvingGen = 1
	for _, id := range []string{"a", "b", "a", "c", "d"} {
		if _, _, _, ok := p.claimResolve(id); ok {
			t.Fatalf("claim of %q must be deferred while a load resolves", id)
		}
	}
	if got := len(p.inflight); got != 0 {
		t.Fatalf("%d resolves started while a load resolves, want 0", got)
	}
	if ids := p.endResolveLocked(2); ids != nil {
		t.Fatalf("superseded load returned deferred ids %v", ids)
	}
	ids := p.endResolveLocked(1)
	if want := []string{"a", "c", "d"}; !slices.Equal(ids, want) {
		t.Fatalf("deferred = %v, want %v", ids, want)
	}
	if _, _, _, ok := p.claimResolve("a"); !ok {
		t.Fatal("claims must run again once the load has resolved")
	}
}

// TestJoinResolvePinsClaim: a Load that joins an in-flight prefetch takes it
// out of the eviction queue, so the prefetches queued right behind the load
// can't cancel the resolve it is waiting on.
func TestJoinResolvePinsClaim(t *testing.T) {
	p := testPlayer()
	defer p.baseCancel()

	ctx, _, done, _ := p.claimResolve("vid")
	join, _ := p.joinResolve("vid")
	if join != done {
		t.Fatal("joinResolve must hand back the in-flight resolve's done channel")
	}
	for i := range maxInflightResolves {
		if _, _, _, ok := p.claimResolve(string(rune('a' + i))); !ok {
			t.Fatal("claim failed")
		}
	}
	if ctx.Err() != nil {
		t.Fatal("a joined resolve must not be evicted")
	}
	if j, _ := p.joinResolve("none"); j != nil {
		t.Fatal("nothing in flight: nothing to join")
	}
}

// TestAwaitResolveCancelReleasesPin: a load superseded while waiting cancels
// the pinned resolve, which nothing else would evict.
func TestAwaitResolveCancelReleasesPin(t *testing.T) {
	p := testPlayer()
	defer p.baseCancel()

	rctx, _, _, _ := p.claimResolve("vid")
	join, gen := p.joinResolve("vid")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, ok := p.awaitResolve(ctx, "vid", join, gen); ok {
		t.Fatal("a cancelled wait must not report a URL")
	}
	if rctx.Err() == nil {
		t.Fatal("the pinned resolve must be cancelled with the load")
	}
	if _, ok := p.inflight["vid"]; ok {
		t.Fatal("the pinned claim must be released")
	}
}
