// Package swarmrally is the gather-then-engage barrier the Scarab Swarm's Kalphite Queen stage
// needs, and nothing else: accounts announce that they are KQ-ready, they WAIT, and they all go
// in together once enough of them are standing by.
//
// The owner's rule, verbatim (2026-09-21): "the accounts should gather when there are a minimum
// of 100 ready to fight kq and then take her on, we should wickedly see a reduced number in
// deaths as kq is unable to hit everyone in the room". This package is the first half of that —
// the gather and the signal. Whether the second half is true is a MEASUREMENT and is not
// assumed anywhere in this file; see cmd/scarabstage3test's kqcrowd phase and the numbers it
// prints.
//
// # Why this is not in internal/mulebus
//
// mulebus is the obvious home — it is this project's in-process pub-sub and it already carries
// KindClaim/KindUnclaim, which are also a crowd-coordination message this project invented
// rather than inherited. It was the wrong home for three reasons, in increasing order of how
// much they matter.
//
//  1. mulebus/bus.go is claimed by another live session in COLLAB.md right now ("mulerestock",
//     additive edits). That alone is a reason not to edit it, but it is the weakest one and it
//     will expire.
//
//  2. mulebus's own doc comment says every Kind name "matches the TS string literals exactly,
//     for anyone cross-referencing the two" against muleProtocol.ts's MuleMessage union. A
//     rally has no counterpart there — MuleReceiver never gathered anybody. KindClaim survives
//     that rule because it is explicitly flagged as not from muleProtocol.ts; a second such
//     exception starts dissolving the property the rule exists to keep.
//
//  3. The load-bearing one: mulebus is a MAILBOX and this is a BARRIER. Its doc draws the line
//     itself, about the loot claims — "This package owns the shape and the delivery; it does
//     not decide anything", with the deciding living in scarabplan/lootclaim.go. A rally is
//     almost entirely deciding: how many ready is enough, what counts as still ready, how long
//     to wait before going in short-handed, and when to give up. Delivering a "I am ready"
//     message is the trivial part. Putting the quorum arithmetic behind a pub-sub would split
//     one small decision across two packages for no gain.
//
// So the split here follows mulebus's own precedent rather than breaking it: Policy/Decide is
// the pure decision (no clock of its own, no locks, unit-tested against a table), and Rally is
// the thin in-process coordinator that counts heartbeats and latches the verdict. Neither
// imports client, actions, fleet or bot, so both are testable without a game.
//
// # The deadlock the owner's rule would otherwise have
//
// "Wait until 100 are ready" with 92 accounts alive is a wait that never ends. Every terminal
// path here is bounded by Policy.MaxWait: once it elapses the rally ALWAYS latches, either
// EngageShort (at least MinEngage are standing by — go in under-strength and say so) or Abort
// (fewer than that — do not walk a handful of accounts into her chamber to be farmed). Wait
// also honours a context, so a caller that is shutting down is never held.
package swarmrally

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
)

// Verdict is what a rally has decided to do.
type Verdict int

const (
	// Gather means keep waiting: the quorum is not met and the deadline has not passed.
	Gather Verdict = iota
	// EngageQuorum means the quorum was reached — the owner's rule, satisfied.
	EngageQuorum
	// EngageShort means MaxWait elapsed with at least MinEngage ready. The swarm goes in
	// under-strength. This is a SEPARATE verdict from EngageQuorum on purpose: a run that
	// engaged short is not a test of the owner's hypothesis, and a report that conflates the
	// two would be claiming a crowd it never had.
	EngageShort
	// Abort means MaxWait elapsed and fewer than MinEngage were ready. Nobody engages.
	Abort
)

func (v Verdict) String() string {
	switch v {
	case Gather:
		return "gather"
	case EngageQuorum:
		return "engage-quorum"
	case EngageShort:
		return "engage-short"
	case Abort:
		return "abort"
	}
	return "unknown"
}

// Engaged reports whether this verdict sends the swarm in.
func (v Verdict) Engaged() bool { return v == EngageQuorum || v == EngageShort }

// Terminal reports whether this verdict is final — a rally never leaves a terminal verdict.
func (v Verdict) Terminal() bool { return v != Gather }

// Policy is the rally's rule. Zero values are not useful; use DefaultPolicy and override.
type Policy struct {
	// Quorum is how many READY accounts send the swarm in. The owner's number is 100.
	Quorum int
	// MinEngage is the floor below which an expired rally aborts instead of engaging
	// under-strength. Below this the swarm is not a crowd, it is a queue of corpses.
	MinEngage int
	// MaxWait bounds the gather. Without it "wait for 100" deadlocks whenever fewer than 100
	// accounts can ever become ready, which is the ordinary case after a bad staging run.
	MaxWait time.Duration
	// Stale is how long a member's last heartbeat stays counted. An account that logged out,
	// died on the way or wedged stops being part of the quorum rather than propping it up
	// forever; this is the difference between "100 are standing by" and "100 once said so".
	//
	// THE CALLER OWES A CADENCE WELL INSIDE THIS. The deadline verdict is taken on the live
	// count at the moment MaxWait expires, so a cohort that went quiet for longer than Stale
	// and woke up past the deadline would abort on however few had heartbeated by then — a
	// correct reading of "nobody is standing by", and a useless one. Heartbeat from a
	// goroutine that does nothing else, so a member busy walking or fighting still reports.
	Stale time.Duration
}

// DefaultPolicy is the owner's rule with bounds that make it terminate.
//
// Quorum 100 is his. MinEngage 20 is the smallest cohort that has ever been measured killing
// her in this repo (the 2026-09-21 farm ran 20). MaxWait 20 minutes is a little over the
// measured ~11.4-minute production KQ cycle, so an account that died on its way to the muster
// has one full cycle to come back before the rally gives up on it. Stale 90s is six of the
// lab's 15-second heartbeats, so an account is dropped for being genuinely gone rather than
// for one missed tick.
func DefaultPolicy() Policy {
	return Policy{Quorum: 100, MinEngage: 20, MaxWait: 20 * time.Minute, Stale: 90 * time.Second}
}

// Valid reports whether a policy can terminate, and why not when it cannot. A rally built from
// an invalid policy is a hang waiting to happen, so New refuses one.
func (p Policy) Valid() error {
	if p.Quorum < 1 {
		return fmt.Errorf("quorum %d: a rally with no quorum engages immediately", p.Quorum)
	}
	if p.MinEngage < 1 {
		return fmt.Errorf("minEngage %d: an expired rally would engage with nobody", p.MinEngage)
	}
	if p.MinEngage > p.Quorum {
		return fmt.Errorf("minEngage %d above quorum %d: the short path would be stricter than the quorum it falls back from", p.MinEngage, p.Quorum)
	}
	if p.MaxWait <= 0 {
		return fmt.Errorf("maxWait %s: a rally with no deadline cannot terminate below quorum", p.MaxWait)
	}
	if p.Stale <= 0 {
		return fmt.Errorf("stale %s: every heartbeat would expire on arrival", p.Stale)
	}
	return nil
}

// Decision is one evaluation of the rule, with the numbers it was made from.
type Decision struct {
	Verdict Verdict
	// Ready is how many members reported ready AND are not stale.
	Ready int
	// Present is how many members are not stale at all, ready or otherwise — the denominator.
	Present int
	// Waited is how long the rally has been open.
	Waited time.Duration
	// Reason is one sentence naming what decided it, for the log and the panel.
	Reason string
}

// Decide is the whole rule, as a pure function of the counts and the clock. Everything else in
// this package exists to feed it.
func Decide(p Policy, ready, present int, waited time.Duration) Decision {
	d := Decision{Ready: ready, Present: present, Waited: waited}
	switch {
	case ready >= p.Quorum:
		d.Verdict = EngageQuorum
		d.Reason = fmt.Sprintf("%d ready reached the quorum of %d after %s", ready, p.Quorum, waited.Round(time.Second))
	case waited < p.MaxWait:
		d.Verdict = Gather
		d.Reason = fmt.Sprintf("%d of %d ready, %s of %s waited", ready, p.Quorum, waited.Round(time.Second), p.MaxWait)
	case ready >= p.MinEngage:
		d.Verdict = EngageShort
		d.Reason = fmt.Sprintf("the %s gather elapsed with %d ready, short of the quorum of %d but at or above the floor of %d — engaging UNDER STRENGTH",
			p.MaxWait, ready, p.Quorum, p.MinEngage)
	default:
		d.Verdict = Abort
		d.Reason = fmt.Sprintf("the %s gather elapsed with only %d ready, below the floor of %d — not engaging",
			p.MaxWait, ready, p.MinEngage)
	}
	return d
}

// member is one account's last heartbeat.
type member struct {
	ready bool
	at    time.Time
	// firstReady is when this member first said ready, so a rally can report how long the
	// earliest arrival stood around — the cost the owner's rule charges for the crowd.
	firstReady time.Time
}

// Rally is the live coordinator: many goroutines heartbeat into it, one verdict comes out, and
// once that verdict is terminal it is LATCHED and never changes.
//
// The latch is not a detail. Accounts start dying the moment they engage, so the ready count
// falls immediately afterwards; without a latch the rally would flip straight back to Gather
// and half the swarm would turn around in her chamber.
type Rally struct {
	p   Policy
	now func() time.Time

	mu      sync.Mutex
	opened  time.Time
	members map[string]member
	latched bool
	last    Decision
	// engagedAt is when the latch closed, zero until then.
	engagedAt time.Time
	// peakReady is the highest ready count ever observed, which is what a report should quote
	// when it says how big the crowd was — the count at the instant of engaging is a sample.
	peakReady int
	// roster is who was present and ready at the instant the verdict latched, sorted by name: the wave's
	// membership, frozen, so every account derives the same seating from it (Roster).
	roster []string
	done   chan struct{}
}

// New opens a rally. now may be nil for time.Now; an injected clock is what makes the tests
// below run in microseconds instead of twenty minutes.
func New(p Policy, now func() time.Time) (*Rally, error) {
	if err := p.Valid(); err != nil {
		return nil, err
	}
	if now == nil {
		now = time.Now
	}
	r := &Rally{p: p, now: now, members: map[string]member{}, done: make(chan struct{})}
	r.opened = now()
	r.last = Decision{Verdict: Gather, Reason: "opened"}
	return r, nil
}

// Report is one account's heartbeat: "I exist, and here is whether I am KQ-ready." Call it on a
// cadence comfortably shorter than Policy.Stale. It is safe from any number of goroutines.
func (r *Rally) Report(name string, ready bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	m := r.members[name]
	m.at = now
	if ready && !m.ready {
		m.firstReady = now
	}
	if !ready {
		m.firstReady = time.Time{}
	}
	m.ready = ready
	r.members[name] = m
	r.evaluateLocked()
}

// Leave removes an account outright — a clean logout, rather than waiting out Policy.Stale.
func (r *Rally) Leave(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.members, name)
	r.evaluateLocked()
}

// Decide re-evaluates and returns the current decision. It is what a caller polls; Report
// already evaluates, so this is mostly for the deadline, which no heartbeat announces.
func (r *Rally) Decide() Decision {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.evaluateLocked()
	return r.last
}

// evaluateLocked runs the rule and latches a terminal verdict. Caller holds r.mu.
func (r *Rally) evaluateLocked() {
	if r.latched {
		return
	}
	now := r.now()
	ready, present := 0, 0
	for _, m := range r.members {
		if now.Sub(m.at) > r.p.Stale {
			continue
		}
		present++
		if m.ready {
			ready++
		}
	}
	if ready > r.peakReady {
		r.peakReady = ready
	}
	r.last = Decide(r.p, ready, present, now.Sub(r.opened))
	if r.last.Verdict.Terminal() {
		r.latched = true
		r.engagedAt = now
		for name, m := range r.members {
			if m.ready && now.Sub(m.at) <= r.p.Stale {
				r.roster = append(r.roster, name)
			}
		}
		sort.Strings(r.roster)
		close(r.done)
	}
}

// Engaged reports whether the swarm has been sent in. False both before the latch and after an
// Abort, which is why callers should read Decide().Verdict rather than only this.
func (r *Rally) Engaged() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.latched && r.last.Verdict.Engaged()
}

// Wait blocks until the rally latches or ctx is done, then returns the decision. It polls at
// `tick` because the deadline is a clock event no heartbeat will announce; pass 0 for 250ms.
//
// It cannot deadlock: a latch is guaranteed by Policy.MaxWait and the ctx is honoured anyway.
// A ctx that ends first returns the current, non-terminal decision — the caller can tell,
// because Verdict.Terminal() is false.
func (r *Rally) Wait(ctx context.Context, tick time.Duration) Decision {
	if tick <= 0 {
		tick = 250 * time.Millisecond
	}
	if d := r.Decide(); d.Verdict.Terminal() {
		return d
	}
	t := time.NewTicker(tick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return r.Decide()
		case <-r.done:
			return r.Decide()
		case <-t.C:
			if d := r.Decide(); d.Verdict.Terminal() {
				return d
			}
		}
	}
}

// MemberState is one account's line in a Snapshot.
type MemberState struct {
	Name  string
	Ready bool
	Stale bool
	// WaitedReady is how long this account has been standing by, zero when it is not ready.
	WaitedReady time.Duration
}

// Snapshot is everything a control panel or a report needs, taken under one lock so the
// numbers in it are consistent with each other.
type Snapshot struct {
	Policy    Policy
	Decision  Decision
	Latched   bool
	PeakReady int
	Elapsed   time.Duration
	// LongestWait is how long the account that has been ready longest has been standing
	// around. It is the price of the owner's rule, and it belongs beside the benefit.
	LongestWait time.Duration
	Members     []MemberState
}

// Snapshot takes a consistent reading.
func (r *Rally) Snapshot() Snapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.evaluateLocked()
	now := r.now()
	s := Snapshot{Policy: r.p, Decision: r.last, Latched: r.latched, PeakReady: r.peakReady,
		Elapsed: now.Sub(r.opened)}
	for name, m := range r.members {
		ms := MemberState{Name: name, Ready: m.ready, Stale: now.Sub(m.at) > r.p.Stale}
		if m.ready && !m.firstReady.IsZero() {
			ms.WaitedReady = now.Sub(m.firstReady)
			if ms.WaitedReady > s.LongestWait {
				s.LongestWait = ms.WaitedReady
			}
		}
		s.Members = append(s.Members, ms)
	}
	sort.Slice(s.Members, func(i, j int) bool { return s.Members[i].Name < s.Members[j].Name })
	return s
}

// Roster is the accounts that were present and ready when the verdict latched, sorted by name, or nil before
// the latch. It is frozen at that instant, so every member of the wave reads the same list however late it asks.
func (r *Rally) Roster() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.roster...)
}
