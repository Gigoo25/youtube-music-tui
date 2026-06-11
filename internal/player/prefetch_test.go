package player

import "testing"

// TestClaimResolveDedup: a videoID can only be claimed once until released, so
// concurrent Prefetch calls never spawn duplicate yt-dlp resolves for it.
func TestClaimResolveDedup(t *testing.T) {
	p := &Player{}
	if !p.claimResolve("vid") {
		t.Fatal("first claim should succeed")
	}
	if p.claimResolve("vid") {
		t.Fatal("second claim while in-flight should be denied (dedup)")
	}
	if !p.claimResolve("other") {
		t.Fatal("a different id should claim independently")
	}
	p.releaseResolve("vid")
	if !p.claimResolve("vid") {
		t.Fatal("after release the id should be claimable again")
	}
}

// TestClaimResolveSkipsCached: an already-cached URL needs no fresh resolve.
func TestClaimResolveSkipsCached(t *testing.T) {
	p := &Player{}
	p.cachePut("vid", "http://stream")
	if p.claimResolve("vid") {
		t.Fatal("a cached id should not be claimed for resolving")
	}
}
