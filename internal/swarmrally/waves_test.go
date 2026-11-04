package swarmrally

import (
	"testing"
	"time"
)

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time { return c.t }

func wavePolicy() Policy {
	return Policy{Quorum: 3, MinEngage: 2, MaxWait: time.Minute, Stale: 30 * time.Second}
}

func TestALatchedGenerationOpensTheNextOnTheNextJoin(t *testing.T) {
	c := &fakeClock{t: time.Unix(1000, 0)}
	w, err := NewWaves(wavePolicy(), c.now)
	if err != nil {
		t.Fatal(err)
	}
	r1, g1 := w.Join()
	if g1 != 1 {
		t.Fatalf("first generation is %d", g1)
	}
	// Two more join while it gathers: the same generation.
	for i := 0; i < 2; i++ {
		if r, g := w.Join(); r != r1 || g != 1 {
			t.Fatalf("a join during the gather opened generation %d", g)
		}
	}
	for _, n := range []string{"a", "b", "c"} {
		r1.Report(n, true)
	}
	if d := r1.Decide(); d.Verdict != EngageQuorum {
		t.Fatalf("three ready did not engage: %s", d.Reason)
	}
	// An account that arrives after the latch — a latecomer, or the next life of a dead one —
	// joins a NEW generation, rather than crossing alone.
	r2, g2 := w.Join()
	if g2 != 2 || r2 == r1 {
		t.Fatalf("after the latch Join gave generation %d (same rally: %v)", g2, r2 == r1)
	}
	if d := r2.Decide(); d.Verdict != Gather {
		t.Fatalf("the new generation starts %s, want gathering", d.Verdict)
	}
	// The latched generation stays latched: an account that joined it keeps its answer.
	if d := r1.Decide(); d.Verdict != EngageQuorum {
		t.Fatalf("generation 1 unlatched to %s", d.Verdict)
	}
}

func TestALaterWaveGetsAFreshMaxWait(t *testing.T) {
	c := &fakeClock{t: time.Unix(1000, 0)}
	w, _ := NewWaves(wavePolicy(), c.now)
	r1, _ := w.Join()
	r1.Report("a", true)
	c.t = c.t.Add(61 * time.Second) // gen 1 times out with one ready: below the floor of 2
	if d := r1.Decide(); d.Verdict != Abort {
		t.Fatalf("an expired generation with 1 ready is %s, want abort", d.Verdict)
	}
	r2, g2 := w.Join()
	if g2 != 2 {
		t.Fatalf("generation %d after an abort", g2)
	}
	c.t = c.t.Add(30 * time.Second)
	if d := r2.Decide(); d.Verdict != Gather {
		t.Fatalf("generation 2, 30s old, is %s: it inherited generation 1's clock", d.Verdict)
	}
	r2.Report("a", true)
	r2.Report("b", true)
	c.t = c.t.Add(31 * time.Second)
	// "a" and "b" reported 31s ago and Stale is 30s, so they no longer count.
	if d := r2.Decide(); d.Verdict != Abort {
		t.Fatalf("stale members propped the quorum: %s (%s)", d.Verdict, d.Reason)
	}
}

func TestSharedWavesAreSharedByName(t *testing.T) {
	defer ForgetWaves("t-waves")
	w1, created, err := SharedWaves("t-waves", wavePolicy())
	if err != nil || !created {
		t.Fatalf("first SharedWaves: created %v err %v", created, err)
	}
	w2, created, _ := SharedWaves("t-waves", Policy{Quorum: 9, MinEngage: 1, MaxWait: time.Second, Stale: time.Second})
	if created || w2 != w1 {
		t.Fatal("a second SharedWaves with the same name made a new sequence")
	}
	if w2.Policy().Quorum != 3 {
		t.Fatal("the second caller's policy replaced the first's")
	}
	if _, _, err := SharedWaves("", wavePolicy()); err == nil {
		t.Fatal("an empty wave name was accepted")
	}
	if _, err := NewWaves(Policy{}, nil); err == nil {
		t.Fatal("an invalid policy was accepted")
	}
}

func TestNoteLatchedCountsOnlyTheCurrentGenerationOnce(t *testing.T) {
	w, _ := NewWaves(wavePolicy(), nil)
	_, g := w.Join()
	w.NoteLatched(g, EngageQuorum)
	w.NoteLatched(g+5, EngageQuorum) // not the current generation
	w.NoteLatched(g, Gather)         // not terminal
	if c := w.Counts(); c[EngageQuorum] != 1 || len(c) != 1 {
		t.Fatalf("counts %v", c)
	}
}

func TestGenStillAnswersForALatchedGenerationAfterTheNextOpens(t *testing.T) {
	c := &fakeClock{t: time.Unix(1000, 0)}
	w, _ := NewWaves(wavePolicy(), c.now)
	if w.Gen(1) != nil {
		t.Fatal("Gen(1) answered before anyone joined")
	}
	r1, _ := w.Join()
	for _, n := range []string{"a", "b", "c"} {
		r1.Report(n, true)
	}
	r1.Decide()
	// A member the latch left behind opens generation 2 on its next pass; a harness watching
	// generation 1 must still find it latched.
	r2, g2 := w.Join()
	if g2 != 2 {
		t.Fatalf("the next join opened generation %d", g2)
	}
	if cur, _ := w.Current(); cur != r2 {
		t.Fatal("Current is not the newest generation")
	}
	if w.Gen(1) != r1 || w.Gen(2) != r2 || w.Gen(3) != nil || w.Gen(0) != nil {
		t.Fatal("Gen does not name the generations it opened")
	}
	if !w.Gen(1).Snapshot().Latched {
		t.Fatal("generation 1 read unlatched through Gen")
	}
	// Bounded: after WavesKept more generations the first is gone.
	for i := 0; i < WavesKept; i++ {
		r, _ := w.Join()
		for _, n := range []string{"a", "b", "c"} {
			r.Report(n, true)
		}
		r.Decide()
	}
	if w.Gen(1) != nil {
		t.Fatal("Gen kept generation 1 past WavesKept")
	}
	if _, g := w.Current(); w.Gen(g) == nil || w.Gen(g-WavesKept+1) == nil {
		t.Fatal("Gen lost a generation inside the kept window")
	}
}
