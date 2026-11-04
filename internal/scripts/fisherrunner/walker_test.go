package fisherrunner

import (
	"testing"

	"rsb0t-go/internal/webwalk"
)

// Live-found 2026-09-18: fisherrunner.Deps had no Walker field at all, so WalkNear's own
// bridge through freshStartDeps always constructed a walker-less freshstartrunner.Deps —
// only a brand-new account's first-day flow got long-distance routing (doors/stairs/ship
// fares), never an established account's ordinary bank-trip navigation. This is a cheap,
// no-session wiring check (freshstartrunner.Deps.Walker is a plain struct field, no live
// session needed to construct or compare it) proving the field now actually threads through,
// not a behavioral proof of WalkNear/WalkWithWalker's own routing logic (already covered by
// freshstartrunner's own tests and this session's live cmd/fisherstrandtest verification).
func TestFreshStartDepsForwardsWalker(t *testing.T) {
	w := &webwalk.Walker{}
	d := &Deps{Walker: w}
	fd := freshStartDeps(d)
	if fd.Walker != w {
		t.Fatalf("freshStartDeps did not forward Walker: got %p want %p", fd.Walker, w)
	}
}

func TestFreshStartDepsNilWalkerStaysNil(t *testing.T) {
	d := &Deps{}
	fd := freshStartDeps(d)
	if fd.Walker != nil {
		t.Fatalf("freshStartDeps invented a Walker from nil: %p", fd.Walker)
	}
}
