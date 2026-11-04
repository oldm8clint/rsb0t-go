// Package mulerunner is the I/O half of the MuleReceiver port: it drives the live-proven
// trade-request-detection/reciprocation/accept loop cmd/muletest first proved against a real
// server, the way barbarianrunner/fisherrunner each extracted their own tool's mechanics so a
// standalone tool and internal/bot's registered script share exactly one implementation
// instead of two copies that can silently drift.
//
// The mechanism, unchanged from cmd/muletest's own doc comment: a single incoming
// "Trade with" request does NOT open the offer screen by itself — the server sends a raw
// "<name>:tradereq:" MESSAGE_GAME line, and the mule must resolve that name to a live player
// index (via gamestate.State.Players()) and call Trade.Request back itself before the offer
// screen opens for either side. Once it opens, accepting the instant it appears races the
// other side still placing their offer — WaitForStableOffer (a real bug found and fixed in
// cmd/muletest, see its own doc comment) waits for the offer to stop changing for a settle
// window before accepting. Do not regress either of these.
//
// Every blocking wait goes through a caller-supplied Delayer instead of time.Sleep directly,
// so a cancellable caller (internal/bot's Context.Delay) can make a running bot actually stop
// mid-wait, while a standalone tool with nothing to cancel can just pass RealDelay — the same
// shape barbarianrunner/fisherrunner settled on.
//
// Scope, deliberately narrow: this package wraps exactly cmd/muletest's own proven mechanism
// (detect a request, reciprocate, accept both trade screens, optionally bank what was
// received nearby) — not internal/scripts/mulereceiverplan's fuller trip-planning/rune-kit/
// PM-order/wall-bus design, which is a separate, much larger integration left for whoever
// picks that up next. Where this package does need a settings shape (the allow-list), it
// reuses mulereceiverplan's own list-parsing convention (WithNames/ListPartners) rather than
// inventing a parallel one — see internal/bot/mule.go.
package mulerunner

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/archive"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/loot"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/randomevents"
	"rsb0t-go/internal/scripts/mulereceiverplan"
	"rsb0t-go/internal/scripts/tile"
)

// Delayer sleeps for d, returning false early (before the full duration elapses) if the
// caller should stop instead of true once d has fully elapsed. Same shape as
// barbarianrunner.Delayer/fisherrunner.Delayer, deliberately — these runner packages are
// siblings.
type Delayer func(d time.Duration) bool

// RealDelay always sleeps the full duration and never cancels — for a standalone tool with
// no runner to request a stop.
func RealDelay(d time.Duration) bool {
	time.Sleep(d)
	return true
}

// Deps is everything one trade cycle needs from the outside world.
type Deps struct {
	Session actions.Session
	// Pack, Locs, Config and Components are all optional — nil disables best-effort
	// banking (BankReceived logs and returns false rather than opening a bank it can't
	// reach or read) exactly mirroring cmd/muletest's own -locs/-config optionality.
	Pack       *pathfind.Pack
	Locs       *locindex.Index
	Config     *cachecfg.Config
	Components archive.Components
	// Chat receives every MESSAGE_GAME line so RequestWatcher can scan it for
	// "<name>:tradereq:" lines without owning its own packet read — the same role
	// barbarianrunner/fisherrunner's own Chat field plays.
	Chat  *chatlog.Log
	Delay Delayer
	// Log receives one already-formatted line per notable event; nil drops them.
	Log func(string)
	// Loot receives the Cooking batches the idle cook rounds make (rangecook.Deps.Loot); nil
	// records nothing. Set from bot.Context.Loot.
	Loot func(loot.Drop)
	// WalkNear, when set, walks toward dest the same door-aware way
	// freshstartrunner.WalkNear does (tryOpenDoor/tryChangeLevel recovery instead of a
	// plain FindPath+Walk) — internal/bot/mule.go wires this to freshstartrunner.WalkNear
	// via muleFreshStartDeps so this package doesn't need to import freshstartrunner
	// directly (the same optional-adapter shape crafterrunner.Deps' own Walker field
	// uses for webwalk). nil is fine for this package's own existing plain walkNear call
	// sites (the bank booth's own immediate neighbourhood, already proven reachable
	// directly) but CookRound requires it: a real door sits across the only route to the
	// Catherby range (see cook.go's own doc comment), and this package's own walkNear has
	// no door-opening at all.
	WalkNear func(dest tile.Tile, radius int, timeout time.Duration) bool
	// IdlePending lets bounded idle cooking yield to a newly arrived trade.
	IdlePending func() bool
	// DeferCook, when set, sees the bank's items as soon as CookRound opens it and returns
	// true to skip the round because other banked work comes first — MuleReceiver's caskets
	// and oysters (owner 2026-09-20: never cook while any are in the bank). nil cooks as before.
	DeferCook func(bankItems []gamestate.SlotItem) bool
	// Stage, when set, is told each stage a trade reaches (mulestatus.Stage* names:
	// reciprocating, offer, accepted, confirm, receipt) with the partner's name, for the
	// control panel's mule dashboard. Reporting only; nil reports nothing.
	Stage func(partner, stage string)
	// OnBankRead, when set, is handed this account's whole bank every time this package has
	// it open and decoded. Reporting only: nothing here reads the result back, and nil (the
	// default) behaves exactly as this package always has.
	//
	// WHY IT EXISTS, from the fleet owner's report of 2026-09-22: "woodcutters are asking
	// the mule for axes they dont even have instock ... they should beable to know whats in
	// their bank to request". A donor cannot read a mule's bank -- it is another session's
	// gamestate, and internal/controlpanel's AllBanks is unreachable from a script because the
	// import graph runs controlpanel -> fleet -> bot (see internal/peerbank's package doc for
	// the full argument). The only way a donor learns what a mule holds is for the mule to
	// PUSH it, and the one moment that costs nothing is the moment the bank is already open.
	// internal/bot/mule.go wires this to bot.PublishBank, which is a single map write into the
	// shared peerbank directory; this package deliberately does not import peerbank itself.
	//
	// Same optional-adapter shape as DeferCook above, and called at the same kind of moment.
	OnBankRead func(bankItems []gamestate.SlotItem)
	// OnBankOpen, when set, is handed the bank's contents on EVERY bank this package opens, and
	// again as it closes it: the deposit of leftovers, a cook round, the openables pass, a key
	// join, a hand-out, a stock read. OnBankRead above fires only after a received haul and in a
	// hand-out, so a mule that had not traded since it started had never published its bank at
	// all, and every donor filtering by it asked blind (2026-09-23, ScarabSwarm's mule-ask entry
	// point). Called often, so the caller rate-limits it. Reporting only; nil does nothing.
	OnBankOpen func(bankItems []gamestate.SlotItem)
	// OnBankClose, when set, is handed the bank's contents as a booth visit closes: the freshest
	// reading the visit has, after every withdrawal and deposit it made. Unlike OnBankOpen the
	// caller should not rate-limit it, or a visit's last word is the one dropped. nil falls back
	// to OnBankOpen.
	OnBankClose func(bankItems []gamestate.SlotItem)
	// Queue, when set, lists the donors that have announced a hand-off to this mule, earliest
	// first: the order RunCycle serves them in (see RequestWatcher.pick). nil serves requesters
	// in the order they asked.
	Queue func() []string
}

// queue is Deps.Queue, nil-safe.
func (d *Deps) queue() []string {
	if d == nil || d.Queue == nil {
		return nil
	}
	return d.Queue()
}

// bankClosing is Deps.OnBankClose, falling back to OnBankOpen when only that is set; nil-safe.
func (d *Deps) bankClosing(items []gamestate.SlotItem) {
	if d == nil || len(items) == 0 {
		return
	}
	if d.OnBankClose != nil {
		d.OnBankClose(items)
		return
	}
	d.bankOpened(items)
}

// bankOpened is Deps.OnBankOpen, nil-safe.
func (d *Deps) bankOpened(items []gamestate.SlotItem) {
	if d == nil || d.OnBankOpen == nil || len(items) == 0 {
		return
	}
	d.OnBankOpen(items)
}

// bankRead is Deps.OnBankRead, nil-safe.
func (d *Deps) bankRead(items []gamestate.SlotItem) {
	if d == nil || d.OnBankRead == nil || len(items) == 0 {
		return
	}
	d.OnBankRead(items)
}

// stage is Deps.Stage, nil-safe.
func (d *Deps) stage(partner, stage string) {
	if d.Stage != nil {
		d.Stage(partner, stage)
	}
}

func (d *Deps) logf(format string, args ...any) {
	if d.Log != nil {
		d.Log(fmt.Sprintf(format, args...))
	}
}

// waitFor polls cond every 100ms via d.Delay until it's true or timeout elapses, returning
// false immediately (without logging a timeout) if d.Delay reports a stop was requested.
// Mirrors barbarianrunner's/fisherrunner's own unexported waitFor.
func waitFor(d *Deps, what string, timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		if !d.Delay(100 * time.Millisecond) {
			return false
		}
	}
	d.logf("timed out waiting for %s", what)
	return false
}

// ---- trade-request detection ----

// TradeRequestFrom reports the sender name when text is a raw incoming trade-request
// notification, ported verbatim from cmd/muletest's own tradeRequestFrom (in turn ported
// from Client.ts's MESSAGE_GAME handler): the server never formats this into "X wishes to
// trade with you" itself, that's purely a client-side display choice — the raw
// "<name>:tradereq:" line is what actually arrives on the wire.
func TradeRequestFrom(text string) (name string, ok bool) {
	const suffix = ":tradereq:"
	text = strings.TrimSpace(text)
	if !strings.HasSuffix(text, suffix) {
		return "", false
	}
	return strings.TrimSuffix(text, suffix), true
}

// RequestWatcher tracks how much of a Deps.Chat log has already been scanned for trade
// requests, so the same "<name>:tradereq:" line is never reciprocated twice by a caller
// that polls it repeatedly (internal/bot's Loop is called over and over by a Runner).
type RequestWatcher struct {
	checkpoint      time.Time
	cursor          int
	pending         []pendingRequest
	awaitingUntil   time.Time
	awaitingPartner string
	// misses counts, per queued donor, trade requests this mule sent FIRST (see pick) that no
	// trade screen followed. A donor that has not answered two is passed over until its own
	// request arrives again, so a donor standing at the post in a bank screen or a dialogue
	// cannot hold the queue.
	misses map[string]int
	// proactive is true while the reservation above is for a first request this mule sent.
	proactive bool
}

type pendingRequest struct {
	name string
	// deadline is reciprocateRetryWindow after the LATEST ask, not the first. A donor waiting
	// its turn re-requests every donorplan.RequestEvery, and until 2026-09-23 that re-ask never
	// refreshed anything: the entry kept its first deadline, so a live donor's entry "expired"
	// ten seconds in however often it asked, and a dead one was indistinguishable from it.
	deadline time.Time
}

// NewRequestWatcher starts watching from now — any trade-request line already sitting in
// the chat log's history before this point is ignored, matching cmd/muletest's own
// behavior of only reacting to requests that arrive after it starts waiting.
func NewRequestWatcher() *RequestWatcher {
	return &RequestWatcher{checkpoint: time.Now()}
}

// Poll returns every trade-request sender name recorded since the last Poll call (oldest
// first), advancing the internal checkpoint so none of them are returned again.
func (w *RequestWatcher) Poll(d *Deps) []string {
	if d.Chat == nil {
		return nil
	}
	lines, next := d.Chat.ReadAfter(w.cursor, w.checkpoint)
	w.cursor = next
	w.checkpoint = time.Time{}
	var names []string
	for _, line := range lines {
		if name, ok := TradeRequestFrom(line); ok {
			names = append(names, name)
		}
	}
	return names
}

// ---- allow-list ----

// AllowList builds the lookup Reciprocate/RunCycle use from a list of partner names,
// matched case-insensitively and whitespace-trimmed — the same normalisation
// cmd/muletest's own -allow flag applied.
// normalisePlayerName folds a name to lowercase and maps underscores to spaces, matching
// classic RS's own login-name/display-name convention: an account's login username uses
// underscores where its in-game display name shows a space (e.g. login "spare_22" displays
// as "Spare 22"). Found live tonight: a partner list built from login-style usernames
// (typed with underscores, matching how the account owner names/refers to their own
// accounts) silently never matched the real in-game names those same accounts' trade
// requests arrive under (with spaces) — every affected partner was rejected indefinitely
// with no error, just "not on the allow list" every single attempt. Normalising both the
// configured allow-list and the observed name through this same function means either
// spelling works regardless of which one an operator happens to type.
func normalisePlayerName(name string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(name)), "_", " ")
}

func AllowList(names []string) map[string]bool {
	out := map[string]bool{}
	for _, n := range names {
		if n = normalisePlayerName(n); n != "" {
			out[n] = true
		}
	}
	return out
}

func isAllowed(allowed map[string]bool, name string) bool {
	return allowed[normalisePlayerName(name)]
}

// QueuedRequests is how many trade requests this watcher is still holding for a partner it
// has reciprocated to but not yet traded with, or could not resolve to a live player index
// yet. Exported so a caller deciding whether the mule is genuinely free (internal/bot's
// muleBot.Loop, before it lets the idle-cooking round start) can tell "nothing arrived in
// the last idle window" apart from "a backlog is still queued" — RunCycle returning
// Handled=false only ever means the former.
//
// Only requests still inside their window count. A donor that is really waiting re-asks every
// donorplan.RequestEvery, which keeps its entry live; one that has walked away stops, and must
// not hold the mule off cooking just because nothing has pruned it yet.
func (w *RequestWatcher) QueuedRequests() int {
	now, n := time.Now(), 0
	for _, p := range w.pending {
		if now.Before(p.deadline) {
			n++
		}
	}
	return n
}

// queue records an ask from name at now: a new entry at the back, or, for a name already
// queued, a later deadline for the entry it already has, keeping its place.
func (w *RequestWatcher) queue(name string, now time.Time) {
	key := normalisePlayerName(name)
	delete(w.misses, key)
	deadline := now.Add(reciprocateRetryWindow)
	for i := range w.pending {
		if normalisePlayerName(w.pending[i].name) == key {
			if deadline.After(w.pending[i].deadline) {
				w.pending[i].deadline = deadline
			}
			return
		}
	}
	w.pending = append(w.pending, pendingRequest{name: name, deadline: deadline})
}

// Forget drops every queued request from name, reporting whether there was one. The mule calls
// it when that donor cancels its hand-off over the bus, so a donor that has given up stops being
// reciprocated to the moment it says so rather than when its window lapses.
//
// It also clears the donor's missed first requests. The count exists to pass over a donor that
// is not answering THIS hand-off, and the accepted-trade branch of RunCycle sets it to the limit
// so a donor that has just finished is not asked again before it cancels. Kept past the cancel,
// that mark outlived the hand-off it was about, and the same donor's NEXT hand-off was never
// asked first: at the head of the queue, beside the mule, its own request turned away while the
// mule was busy, it waited out its cap (review of main 56d3a09d..4733c7da: 17 of 65 archived
// give-ups were donors the mule had already traded with and never saw a request from).
func (w *RequestWatcher) Forget(name string) bool {
	key := normalisePlayerName(name)
	delete(w.misses, key)
	kept, dropped := w.pending[:0], false
	for _, p := range w.pending {
		if normalisePlayerName(p.name) == key {
			dropped = true
			continue
		}
		kept = append(kept, p)
	}
	w.pending = kept
	return dropped
}

// traded is what an accepted trade does to the watcher: the partner's queued requests go
// (Forget), and THEN it is marked as just served (NoteServed). The order matters: Forget clears
// the donor's misses, so the other way round would undo the mark at once and the mule would ask a
// donor that has just finished to trade again.
func (w *RequestWatcher) traded(partner string) {
	w.Forget(partner)
	w.NoteServed(partner)
}

// NoteServed marks name as just traded with: it is not asked to trade again until its hand-off
// ends (Forget) or a new one is announced (Announced). RunCycle calls it after every accepted
// trade, after Forget.
func (w *RequestWatcher) NoteServed(name string) {
	if w.misses == nil {
		w.misses = map[string]int{}
	}
	w.misses[normalisePlayerName(name)] = maxProactiveMisses
}

// MissedFirstRequests is how many of this mule's own first requests name has left unanswered in
// its current hand-off (maxProactiveMisses once it has just traded). Reporting and tests only.
func (w *RequestWatcher) MissedFirstRequests(name string) int {
	return w.misses[normalisePlayerName(name)]
}

// Announced clears name's missed first requests when it announces a NEW hand-off (KindReady): a
// fresh hand-off starts with a clean slate, whatever the last one left behind. See Forget.
func (w *RequestWatcher) Announced(name string) {
	delete(w.misses, normalisePlayerName(name))
}

// Prune drops every queued request that is both past its window and from a player no longer in
// view, and says so in one line. See prune.
func (w *RequestWatcher) Prune(d *Deps) int { return len(w.prune(d, time.Now())) }

// prune is the one place queued requests die, and it runs on EVERY RunCycle tick and after every
// hand-out, not only on a quiet tick.
//
// THE FLUSH THIS ENDS, live 2026-09-23 00:50:32 on 263cceda: 51 "giving up reciprocating ...
// never became visible" lines in one second, every one naming a donor that had ended its hand-off
// between 42 seconds and 10 minutes earlier. The deadline was only checked on a tick where nothing
// had been reciprocated yet; under load every RunCycle's first tick reciprocated a fresh requester,
// so the list was carried forward unchecked until the first quiet tick walked all of it at once.
// The hand-out path (DropRequestsFrom, with no RunCycle between hand-outs) queued without ever
// checking either.
//
// Both halves of the test matter. Past the window alone would drop a donor standing beside the
// mule that is briefly missing from PLAYER_INFO; out of view alone would drop one that asked a
// moment ago and has not been drawn yet.
func (w *RequestWatcher) prune(d *Deps, now time.Time) []string {
	var dropped []string
	kept := w.pending[:0]
	for _, p := range w.pending {
		if !now.Before(p.deadline) {
			if _, visible := ResolvePlayerSlot(d, p.name); !visible {
				dropped = append(dropped, p.name)
				continue
			}
		}
		kept = append(kept, p)
	}
	w.pending = kept
	if len(dropped) > 0 {
		d.logf("dropped %d stale trade request(s), no longer in view: %s", len(dropped), strings.Join(dropped, ", "))
	}
	return dropped
}

// pick chooses who to trade with next: the earliest-announced donor in the queue (Deps.Queue)
// that is standing beside this mule, whether or not its own request has got through; failing
// that, the queued requester in view that asked first. idx is its entry in pending, or -1 when
// the mule is to send the first request itself. name is "" when there is nobody to serve.
//
// NOT THE FIRST LINE IN THE CHAT LOG, which is what it used to be. Every waiting donor re-requests
// every three seconds, so "first line seen" is effectively random: live 263cceda, 15 of 25
// services went to a donor with somebody who announced earlier still waiting, one overtook 19; on
// 3dfb543a, 14 of 18.
//
// AND NOT ONLY AMONG REQUESTS THAT ARRIVED, which is what the first version of this did and the
// lab proved useless: the engine refuses a trade request to a player who is busy ("Hofq2a is busy
// at the moment." in the donor's chat) and never delivers it, and a mule is busy -- trading or
// banking -- nearly all the time a queue exists. So the donors' requests only land in the gaps,
// one or two at a time, and ordering those orders nothing: the lab served d1, d3, d4, d2. The mule
// asking the head of the queue itself needs no gap. The donor re-requests every
// donorplan.RequestEvery, which opens the screen inside the five-second reservation RunCycle
// keeps for it. Only a donor within reach (meetReach) is asked this way, because a trade request
// walks the requester to its target and a mule must not leave its post to chase one.
func (w *RequestWatcher) pick(d *Deps) (idx int, name string) {
	awaiting := normalisePlayerName(w.awaitingPartner)
	pendingAt := func(key string) int {
		for i, p := range w.pending {
			if normalisePlayerName(p.name) == key {
				return i
			}
		}
		return -1
	}
	for _, queued := range d.queue() {
		key := normalisePlayerName(queued)
		if key == "" || key == awaiting {
			continue
		}
		i := pendingAt(key)
		if i >= 0 {
			if _, visible := ResolvePlayerSlot(d, w.pending[i].name); visible {
				return i, w.pending[i].name
			}
			continue
		}
		if w.misses[key] >= maxProactiveMisses {
			continue
		}
		// The name as the game shows it, not the queue key: it is what the trade screen will
		// say, so the reservation, the log and the dashboard name the same player one way.
		if shown, near := playerNear(d, queued, meetReach); near {
			return -1, shown
		}
	}
	for i, p := range w.pending {
		if normalisePlayerName(p.name) == awaiting {
			continue
		}
		if _, visible := ResolvePlayerSlot(d, p.name); visible {
			return i, p.name
		}
	}
	return -1, ""
}

// askFirst sends the first trade request to name, the queued donor next in line whose own request
// has not got through (see pick). Same allow-list rule as Reciprocate.
func askFirst(d *Deps, name string, allowed map[string]bool) bool {
	if !isAllowed(allowed, name) {
		return false
	}
	slot, ok := ResolvePlayerSlot(d, name)
	if !ok {
		return false
	}
	d.logf("asking %q to trade: it is next in the queue", name)
	if err := actions.NewTrade(d.Session).Request(slot); err != nil {
		d.logf("asking %q to trade: %v", name, err)
		return false
	}
	return true
}

// meetReach is how close a queued donor must stand for this mule to send it the first request:
// donorplan.MeetRange, the distance a donor itself trades from.
const meetReach = 2

// maxProactiveMisses is how many first requests a queued donor may leave unanswered before the
// mule waits for that donor's own request instead.
const maxProactiveMisses = 2

// playerNear reports whether the player called name (under any spelling normalisePlayerName
// folds together) is on this account's level within tiles of it, and if so the name the game
// shows for it.
func playerNear(d *Deps, name string, tiles int) (shown string, near bool) {
	x, z, level, ok := d.Session.State().Tile()
	if !ok {
		return "", false
	}
	key := normalisePlayerName(name)
	for _, p := range d.Session.State().Players() {
		if normalisePlayerName(p.Name) == key {
			if p.Level == level && abs(p.X-x) <= tiles && abs(p.Z-z) <= tiles {
				return p.Name, true
			}
			return "", false
		}
	}
	return "", false
}

func (w *RequestWatcher) remove(i int) {
	w.pending = append(w.pending[:i], w.pending[i+1:]...)
}

// DropRequestsFrom consumes every trade-request line sitting in the chat log right now,
// discarding the ones naming name and queueing the rest for RunCycle to reciprocate as usual.
//
// Needed by the hand-OUT direction (see handout.go). There, the mule opens the trade and the
// DONOR reciprocates — which puts a perfectly ordinary "<donor>:tradereq:" line in this
// account's own chat log for a trade that has already happened. Left there, the very next
// RunCycle would reciprocate it and sit on an empty offer screen until completeTrade's own
// 20-second settle timeout gave up: not a correctness bug, but a full wasted cycle per
// hand-out, and a confusing "not completed" line in the log for a trade that in fact succeeded.
// Discarding only that one name (rather than draining the whole log) keeps every other donor's
// genuine request queued, which is why this exists instead of a bare Poll-and-throw-away.
func (w *RequestWatcher) DropRequestsFrom(d *Deps, name string) int {
	dropped, now := 0, time.Now()
	for _, seen := range w.Poll(d) {
		if normalisePlayerName(seen) == normalisePlayerName(name) {
			dropped++
			continue
		}
		w.queue(seen, now)
	}
	w.prune(d, now)
	return dropped
}

// ---- reciprocation ----

// ResolvePlayerSlot turns a name into a live PLAYER_INFO index via
// gamestate.State.Players(), ported from cmd/muletest's own resolvePlayerSlot (which was
// itself a placeholder until Players() landed — this is the real implementation cmd/muletest
// now uses).
func ResolvePlayerSlot(d *Deps, name string) (int32, bool) {
	// normalisePlayerName, so a login name ("big_net", what a bus message carries and what a
	// hand-out resolves) finds the player the game shows as "Big Net".
	target := normalisePlayerName(name)
	for _, p := range d.Session.State().Players() {
		if normalisePlayerName(p.Name) == target {
			return int32(p.Index), true
		}
	}
	return 0, false
}

// Reciprocate is what a real MuleReceiver must do on a trade request: resolve the sender's
// name to their live player index and reciprocate with Trade.Request, since the offer
// screen never opens from one side alone (confirmed live — see this package's doc comment).
// Returns false without sending anything if name isn't on the allow list or can't be
// resolved to a live player.
func Reciprocate(d *Deps, name string, allowed map[string]bool) bool {
	if !isAllowed(allowed, name) {
		d.logf("trade request from %q — not on the allow list, ignoring", name)
		return false
	}
	d.logf("trade request from %q — reciprocating", name)
	slot, ok := ResolvePlayerSlot(d, name)
	if !ok {
		d.logf("cannot reciprocate %q's trade request: not found among visible players", name)
		return false
	}
	if err := actions.NewTrade(d.Session).Request(slot); err != nil {
		d.logf("reciprocating trade request to %q: %v", name, err)
		return false
	}
	return true
}

// ---- accepting a trade ----

// WaitForStableOffer polls the other side's offer until it stops changing for settleFor, or
// timeout elapses — whichever comes first. This is the real bug fix cmd/muletest's own doc
// comment describes: accepting on the very first offer-screen snapshot beat the other side
// placing their items, which un-accepted and reopened the screen. Do not regress this by
// accepting as soon as OnOfferScreen/OnConfirmScreen goes true.
func WaitForStableOffer(d *Deps, trade *actions.Trade, timeout, settleFor time.Duration) []gamestate.SlotItem {
	deadline := time.Now().Add(timeout)
	last := trade.TheirOffer()
	lastGeneration := offerGeneration(d)
	lastChange := time.Now()
	for time.Now().Before(deadline) {
		if randomevents.Busy(d.Session) {
			return nil
		}
		if !trade.OnOfferScreen() {
			// Same transient-flicker class completeTrade's own confirm-wait loop
			// debounces (see stillClosed's doc comment) — live-found 2026-09-18
			// independently of that case (no random event on the mule's own side,
			// see COLLAB.md's mule trade investigation entry): "trade offer
			// screen open with X" immediately followed, same tick, by "no
			// nonempty stable offer arrived — declining", i.e. this early-exit
			// firing on the very first poll rather than after any real wait.
			if stillClosed(d, trade.OnOfferScreen) {
				return nil
			}
		}
		if !d.Delay(250 * time.Millisecond) {
			return nil
		}
		cur := trade.TheirOffer()
		generation := offerGeneration(d)
		if generation != lastGeneration || !sameOffer(last, cur) {
			lastGeneration = generation
			last, lastChange = cur, time.Now()
			continue
		}
		if len(cur) > 0 && trade.OnOfferScreen() && time.Since(lastChange) >= settleFor {
			return cur
		}
	}
	return nil
}

// debounceWindow is how long a single false trade.Active()/OnOfferScreen() reading is
// given to reverse itself before being trusted as a genuine close. Live-found
// 2026-09-18 (real production evidence, see COLLAB.md's mule trade investigation
// entry): dozens of "not completed" trade failures against many different donors, right
// after a fleet restart, where the very next poll (sometimes the very next RunCycle
// call) found the SAME partner's trade screen open again — proving the interface never
// actually closed. Since Decline() is itself a no-op when the trade isn't Active() (see
// its own doc comment), reacting to that single false reading only ever threw away an
// otherwise-good, already-in-progress trade for nothing.
const debounceWindow = 600 * time.Millisecond

// stillClosed polls activeNow (trade.Active or trade.OnOfferScreen — whichever the
// caller just saw report false) for up to debounceWindow, returning false the moment it
// reports true again (a flicker, not a real close) and true only once the whole window
// elapses without that happening. Also returns true if the caller should stop (d.Delay
// itself returns false), since there's nothing left to wait for either way.
func stillClosed(d *Deps, activeNow func() bool) bool {
	for deadline := time.Now().Add(debounceWindow); time.Now().Before(deadline); {
		if !d.Delay(100 * time.Millisecond) {
			return true
		}
		if activeNow() {
			return false
		}
	}
	return true
}

func offerGeneration(d *Deps) int {
	if inv := d.Session.State().Inventory(actions.TradeTheirOffer); inv != nil {
		return inv.Generation
	}
	return 0
}

func freshReceivedInventory(inv *gamestate.Inventory, previousGeneration int, before map[int32]int64, received []gamestate.SlotItem) bool {
	return inv != nil && inv.FullGeneration > previousGeneration && receivedInventoryDelta(inv.NonEmpty(), before, received)
}

// Compare actual physical inventory totals, including duplicate slots.
func receivedInventoryDelta(items []gamestate.SlotItem, before map[int32]int64, received []gamestate.SlotItem) bool {
	after, want := make(map[int32]int64), make(map[int32]int64)
	for _, item := range items {
		after[item.ID] += int64(item.Count)
	}
	for _, item := range received {
		want[item.ID] += int64(item.Count)
	}
	for id, count := range want {
		if after[id] < before[id]+count {
			return false
		}
	}
	return len(want) > 0
}

// offerCovers reports whether got holds at least every item count in want, by item id.
func offerCovers(got, want []gamestate.SlotItem) bool {
	if len(want) == 0 {
		return false
	}
	have := make(map[int32]int64)
	for _, it := range got {
		have[it.ID] += int64(it.Count)
	}
	for _, it := range want {
		have[it.ID] -= int64(it.Count)
	}
	for _, n := range have {
		if n < 0 {
			return false
		}
	}
	return true
}

func sameOffer(a, b []gamestate.SlotItem) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].ID != b[i].ID || a[i].Count != b[i].Count {
			return false
		}
	}
	return true
}

// confirmWait is how long completeTrade waits for the confirm screen after its latest accept of
// a settled offer; confirmHardCap bounds the whole offer stage however often the offer grows.
// stableOfferWait is how long the mule waits for a donor's FIRST offer to settle.
//
// IT WAS 20 SECONDS, AND THAT IS EXACTLY THE PROBLEM. CLAUDE.md's own mule-throughput row
// measures the donor's half of the cycle at "20s the donor spends drawing and settling its
// offer (the mule only waits)". A 20-second timeout on a 20-second median is a coin flip: about
// half of all trades lose it, through no fault of either side.
//
// That is what the live log shows, and the failures land in the same SECOND as the success:
//
//	22:51:43 [mule]  trade with "Acct80" not completed (declined, or interrupted...)
//	22:51:43 [acct80] donor: trade with "mule" reached the confirm screen
//	22:51:44 [mule]  interrupted or no nonempty stable offer arrived — declining
//
// acct121 produced the identical pattern an hour earlier with 19 stacks on the table, and its
// retry 35 seconds later succeeded unchanged -- which is the proof the offer was fine and only
// the clock was wrong.
//
// Raising it is close to free. settleFor still returns the moment an offer has been unchanged
// for 1.2s, so a fast donor is served exactly as fast as before; the timeout only ever bites a
// donor that is still drawing. The one cost is a donor that vanishes mid-offer, which now holds
// the mule for longer -- bounded well under confirmHardCap below, and far cheaper than throwing
// away a completed draw, since a failed engagement costs the donor its whole 35-second cycle
// and the mule is the fleet's throughput bottleneck during a restart ramp.
//
// 45s is a little over twice the measured median and still only a third of the hard cap.
const stableOfferWait = 45 * time.Second

const (
	confirmWait    = 30 * time.Second
	confirmHardCap = 2 * time.Minute
)

// CycleResult is the outcome of one RunCycle call.
type CycleResult struct {
	// Handled is false only when the whole timeout elapsed with no trade request seen at
	// all — a caller should just treat that as "nothing happened this call" rather than
	// logging a failure. Every other outcome (declined, interrupted mid-trade, or a full
	// accept) sets Handled true.
	Handled bool
	// Accepted is true only once both trade screens were confirmed and the trade closed.
	Accepted bool
	// Partner is the other side's name, once the offer screen opened enough to learn it.
	Partner string
	// Received is what came in, set only when Accepted is true.
	Received []gamestate.SlotItem
	// Reason is a short, stable description of why a handled trade was not accepted, for the
	// mule dashboard's failure list. Empty when Accepted, or when nothing was handled.
	Reason string
}

// completeTrade drives an already-open offer screen through to a close: partner-name
// re-check (defense in depth — Reciprocate already checked the allow list before ever
// sending Trade.Request, but this re-checks against the server's own idea of who is on the
// other side of the screen), WaitForStableOffer, then both Accept/confirm steps, ported
// directly from cmd/muletest's own main loop body.
//
// expectedPartner is who RunCycle just reciprocated (watcher.awaitingPartner, captured
// before it's cleared) — "" if unknown. Live-found 2026-09-18: Trade.Partner() reads
// gamestate.State.Text(TradePartner), a component text value that IF_SETTEXT sets but
// nothing ever clears when the trade interface closes, so it keeps whatever the LAST trade's
// partner name was until a fresh IF_SETTEXT for this specific trade actually lands. The old
// `waitFor(..., func() bool { return trade.Partner() != "" })` was a no-op after an account's
// very first trade of the session for exactly this reason — the field is never genuinely
// empty again, only possibly stale. Confirmed live: the mule logged "trade offer screen open
// with 'Lost'" twice in the same second, immediately after declining a real trade with Lost
// and moving on to reciprocate Acct11 — but Lost's own donor-side log shows only ONE real
// trade that whole window. The second "Lost" was a phantom stale-text read of a trade that
// was actually Acct11's, misidentified as an empty/interrupted trade with Lost and wrongly
// declined, wasting a full RunCycle iteration and delaying whoever was really queued next.
// Waiting here for Partner() to actually match who we expect (falling through to whatever's
// there if it never does within the timeout, so an unexpected/unprompted trade — which
// shouldn't happen per this package's own doc comment, but isn't impossible — still gets
// handled rather than stalling forever) closes that window without touching the shared,
// widely-used gamestate.State.Text mechanism itself.
//
// The match is by normalisePlayerName, never ==. expectedPartner can be in queue-key form
// (Deps.Queue is muleKey form, "hofq10d2" or "big net") or login form ("big_net"), while the
// screen shows the display name ("Hofq10d2", "Big Net"). Found by review 2026-09-23: this used
// ==, so every trade the mule asked for itself (RequestWatcher.pick's first-request path) sat
// here the whole three seconds and logged "timed out waiting for trade partner name" before
// reading the partner -- on the one mule the fleet queues for, on the path a queue takes most.
// A stale header from the previous trade is still told apart: it names somebody else under any
// spelling.
func completeTrade(d *Deps, trade *actions.Trade, allowed map[string]bool, expectedPartner string) CycleResult {
	want := normalisePlayerName(expectedPartner)
	waitFor(d, "trade partner name", 3*time.Second, func() bool {
		p := trade.Partner()
		return p != "" && (want == "" || normalisePlayerName(p) == want)
	})
	partner := trade.Partner()
	d.logf("trade offer screen open with %q", partner)
	d.stage(partner, "offer")

	if !isAllowed(allowed, partner) {
		d.logf("%q not on the allow list — declining", partner)
		_ = trade.Decline(3 * time.Second)
		return CycleResult{Handled: true, Partner: partner, Reason: "partner not on the allow list"}
	}

	offer := WaitForStableOffer(d, trade, stableOfferWait, 1200*time.Millisecond)
	if len(offer) == 0 {
		d.logf("trade with %q interrupted or no nonempty stable offer arrived — declining", partner)
		_ = trade.Decline(3 * time.Second)
		return CycleResult{Handled: true, Partner: partner, Reason: "no stable offer"}
	}
	d.logf("offer settled: %d item stack(s)", len(offer))
	for _, it := range offer {
		d.logf("  id=%d count=%d", it.ID, it.Count)
	}

	acceptedGeneration := offerGeneration(d)
	if err := trade.Accept(); err != nil {
		d.logf("accept (offer screen): %v", err)
		return CycleResult{Handled: true, Partner: partner, Reason: "offer accept failed"}
	}
	d.stage(partner, "accepted")
	// 8s -> 20s -- live-found 2026-09-16, this codebase's first real production run:
	// the mule timed out waiting for the confirm screen against the SAME single partner
	// (spare24) three times in a row, each retry re-offering identically and each
	// eventually reaching "offer settled" fine — not the rotating-cast-of-partners bug
	// this function's own RunCycle doc comment already covers (that's a corrupted-screen
	// case; this was a clean single partner just taking longer than 8s to transition).
	// Same class of gap as the bank-open and sail-completion timeouts already raised
	// tonight: every wait in this file was tuned against the local Lost City test engine's
	// near-zero latency, not a real remote server's round-trip under real load.
	confirmDeadline := time.Now().Add(confirmWait)
	// A donor handing over a big load (caskets and oysters opened, doubled thresholds) draws
	// ~20 stacks one at a time for over 30 seconds. Live 2026-09-19 on f41f140: the fixed 30s
	// deadline declined those trades mid-offer at exactly 30s and took the next donor, so
	// nothing moved. Each time the offer grows the deadline slides forward again, bounded by
	// confirmHardCap so a partner that never stops changing still ends.
	hardDeadline := time.Now().Add(confirmHardCap)
	for !trade.OnConfirmScreen() {
		// Live-found 2026-09-18: this whole loop used to collapse four very different
		// bail reasons (trade genuinely gone, our own random event, plain timeout, a
		// stop request) into one unlabelled trade.Decline() — every one of them then
		// surfaced identically at the caller as "not completed (declined, or
		// interrupted before it closed)". Real production evidence that night showed
		// dozens of these against many different donors (not one partner) with no way
		// to tell which case actually fired; logging each distinctly here is step one
		// of actually finding out. See COLLAB.md's mule trade investigation entry.
		if randomevents.Busy(d.Session) {
			d.logf("trade with %q interrupted by a random event before reaching the confirm screen — declining", partner)
			_ = trade.Decline(3 * time.Second)
			return CycleResult{Handled: true, Partner: partner, Reason: "random event before confirm"}
		}
		if !time.Now().Before(confirmDeadline) {
			d.logf("timed out waiting for the confirm screen with %q — declining", partner)
			_ = trade.Decline(3 * time.Second)
			return CycleResult{Handled: true, Partner: partner, Reason: "confirm screen timeout"}
		}
		if !d.Delay(250 * time.Millisecond) {
			return CycleResult{Handled: true, Partner: partner, Reason: "stopped"}
		}
		if !trade.Active() {
			// A single false read here can be a transient interface refresh (the
			// server momentarily closing/reopening the trade component around an
			// offer change) rather than a genuine close. Live evidence: the mule
			// logged "not completed" for a partner and then, the very same or
			// next tick, "trade offer screen open" with that SAME partner again
			// — the interface never actually went away. Decline() itself is a
			// no-op when !Active() (see its own doc comment), so bailing
			// instantly here on a false reading throws away an otherwise-good,
			// already-settled trade for nothing. Debounce with a short re-check
			// before believing it.
			reactivated := false
			for recheckDeadline := time.Now().Add(600 * time.Millisecond); time.Now().Before(recheckDeadline); {
				if !d.Delay(100 * time.Millisecond) {
					return CycleResult{Handled: true, Partner: partner, Reason: "stopped"}
				}
				if trade.Active() {
					reactivated = true
					break
				}
			}
			if !reactivated {
				d.logf("trade with %q closed before reaching the confirm screen — declining", partner)
				_ = trade.Decline(3 * time.Second)
				return CycleResult{Handled: true, Partner: partner, Reason: "closed before confirm"}
			}
			d.logf("trade interface briefly went inactive but %q's trade is still open — continuing", partner)
		}
		if trade.OnOfferScreen() && (acceptedGeneration != offerGeneration(d) || !sameOffer(offer, trade.TheirOffer())) {
			// Any change resets server-side acceptance. Wait for the new offer
			// to settle before accepting again, rather than wedging this screen.
			settled := offer
			offer = WaitForStableOffer(d, trade, time.Until(hardDeadline), 1200*time.Millisecond)
			if len(offer) == 0 && trade.OnConfirmScreen() {
				// The generation also moves when the server re-sends an unchanged offer.
				// Any real change clears both accepts server-side, so reaching the confirm
				// screen means our accept of the settled offer stood. The confirm check
				// below still compares what we would receive against it. Live 2026-09-19:
				// this was the most common coded decline reason, 5 of ~20 failed trades.
				d.logf("trade with %q reached the confirm screen while re-settling — checking it against the settled offer", partner)
				offer = settled
				continue
			}
			if len(offer) == 0 {
				d.logf("trade with %q changed but no new stable nonempty offer arrived before the confirm screen — declining", partner)
				_ = trade.Decline(3 * time.Second)
				return CycleResult{Handled: true, Partner: partner, Reason: "offer changed, never re-settled"}
			}
			d.logf("offer changed — accepting the new settled offer (%d stack(s))", len(offer))
			if next := time.Now().Add(confirmWait); next.After(confirmDeadline) {
				confirmDeadline = next
				if confirmDeadline.After(hardDeadline) {
					confirmDeadline = hardDeadline
				}
			}
			acceptedGeneration = offerGeneration(d)
			if trade.Accept() != nil {
				return CycleResult{Handled: true, Partner: partner, Reason: "offer re-accept failed"}
			}
		}
	}
	mine, theirs := trade.ConfirmOffers()
	// A donor still drawing from its bank can add stacks after our accept lands, and its own
	// accept then opens a confirm screen holding more than we settled on. Live 2026-09-19 on
	// 3219bcf: 3 of 14 failed trades were exactly that (3 -> 6 -> 8 -> 10 stacks). We give
	// nothing, so receiving at least everything settled is never worse; receipt is still
	// checked against the confirm screen. Empty, shrunk or swapped offers are still declined.
	if len(mine) == 0 && trade.Partner() == partner && !sameOffer(offer, theirs) && offerCovers(theirs, offer) {
		d.logf("confirm screen with %q holds %d stack(s), a superset of the %d settled — accepting it", partner, len(theirs), len(offer))
		offer = theirs
	}
	if len(mine) != 0 || !sameOffer(offer, theirs) || trade.Partner() != partner {
		d.logf("confirmation does not match the settled incoming offer with %q (mine=%d matchesTheirs=%v partner=%q) — declining",
			partner, len(mine), sameOffer(offer, theirs), trade.Partner())
		_ = trade.Decline(3 * time.Second)
		return CycleResult{Handled: true, Partner: partner, Reason: "confirm screen did not match the offer"}
	}
	d.logf("confirming: giving %d stack(s), receiving %d stack(s)", len(mine), len(theirs))
	before := make(map[int32]int64)
	for _, item := range trade.SidePack() {
		before[item.ID] += int64(item.Count)
	}
	if err := trade.Accept(); err != nil {
		d.logf("accept (confirm screen): %v", err)
		return CycleResult{Handled: true, Partner: partner, Reason: "confirm accept failed"}
	}
	d.stage(partner, "confirm")
	if !waitFor(d, "trade closed", 20*time.Second, func() bool { return !trade.Active() }) {
		return CycleResult{Handled: true, Partner: partner, Reason: "trade never closed after confirm"}
	}
	d.stage(partner, "receipt")
	// A cancelled confirm screen also closes. Count a completed handoff only
	// after the account's actual inventory gained every promised stack.
	receivedOK := waitFor(d, "received trade inventory", 5*time.Second, func() bool {
		return receivedInventoryDelta(packItems(d), before, theirs)
	})
	if !receivedOK && !randomevents.Busy(d.Session) {
		// Some sessions do not expose a current normal-backpack snapshot.
		// Opening the bank requests an actual deposit-side inventory snapshot;
		// never infer receipt just from the trade interface closing.
		generations := make(map[int]int)
		for id, c := range d.Components {
			if c != nil && c.Type == archive.TypeInv {
				if inv := d.Session.State().Inventory(id); inv != nil {
					generations[id] = inv.Generation
				}
			}
		}
		if bank, opened := openNearestBankBooth(d); opened {
			receivedOK = waitFor(d, "received trade bank inventory", 5*time.Second, func() bool {
				id, snapshot := bank.InventorySnapshot()
				return freshReceivedInventory(snapshot, generations[id], before, theirs)
			})
			_ = bank.Close()
		}
	}
	if !receivedOK {
		d.logf("trade closed without confirmed received items with %q", partner)
		return CycleResult{Handled: true, Partner: partner, Reason: "closed without the items arriving"}
	}
	d.logf("TRADE ACCEPTED with %q", partner)
	return CycleResult{Handled: true, Accepted: true, Partner: partner, Received: theirs}
}

// reciprocateRetryWindow bounds how long RunCycle keeps retrying to resolve a just-seen
// requester's name to a live player index before giving up on that request entirely.
// Found live, not assumed: a fresh arrival's PLAYER_INFO update can lag a tick or two
// behind the MESSAGE_GAME ":tradereq:" line naming them (both accounts freshly
// teleported into view of each other in the same round of ticks), so ResolvePlayerSlot's
// very first attempt can fail even though the request is entirely genuine and the sender
// becomes visible moments later. A single-shot lookup (matching cmd/muletest's own
// original resolvePlayerSlot) would silently drop that request forever.
const reciprocateRetryWindow = 10 * time.Second

// RunCycle waits up to timeout for one full trade to play out — an incoming trade request
// from an allowed partner, reciprocated, both screens accepted — polling the chat log for
// new requests and the trade's own offer-screen state on the same tick, since (unlike
// cmd/muletest, which had a second goroutine reading raw packets independently of its main
// accept loop) a Runner-driven Loop call has only this one call to do both jobs in.
//
// Returns as soon as a trade is fully handled one way or another (accepted, declined, or
// interrupted mid-flight by a stop request); returns with Handled=false only if timeout
// elapses with no trade request seen at all.
func RunCycle(d *Deps, watcher *RequestWatcher, allowed map[string]bool, timeout time.Duration) CycleResult {
	trade := actions.NewTrade(d.Session)
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if trade.Active() {
			expectedPartner := watcher.awaitingPartner
			delete(watcher.misses, normalisePlayerName(expectedPartner))
			watcher.awaitingUntil = time.Time{}
			watcher.awaitingPartner, watcher.proactive = "", false
			result := completeTrade(d, trade, allowed, expectedPartner)
			if result.Accepted {
				// The partner stays in the caller's queue until it cancels (a load that needs a
				// second screen keeps its place), but it is not ASKED again: a donor that has
				// just finished is about to cancel, and one with more to hand over asks itself
				// once its post-trade cooldown ends, which puts it straight back at the head.
				watcher.traded(result.Partner)
				now := time.Now()
				for _, name := range watcher.Poll(d) {
					if isAllowed(allowed, name) && normalisePlayerName(name) != normalisePlayerName(result.Partner) {
						watcher.queue(name, now)
					}
				}
			}
			return result
		}
		now := time.Now()
		// awaiting caps outgoing requests at ONE until the reciprocated screen opens or its
		// five seconds pass. Found live in production (2026-09-14): a burst of simultaneous
		// requests after a reconnect fired a reciprocating Trade.Request to every donor in the
		// same tick, which corrupted whichever screen actually opened ("timed out waiting for
		// confirm screen" against a rotating cast of partners, none of them completing).
		awaiting := now.Before(watcher.awaitingUntil)
		for _, name := range watcher.Poll(d) {
			if awaiting && normalisePlayerName(name) == normalisePlayerName(watcher.awaitingPartner) {
				continue
			}
			if !isAllowed(allowed, name) {
				Reciprocate(d, name, allowed) // logs "not on the allow list, ignoring" once
				continue
			}
			watcher.queue(name, now)
		}
		watcher.prune(d, now)
		if !awaiting {
			if watcher.awaitingPartner != "" {
				// The last reservation lapsed with no screen. If it was a first request
				// this mule sent, count it against that donor.
				if key := normalisePlayerName(watcher.awaitingPartner); watcher.proactive {
					if watcher.misses == nil {
						watcher.misses = map[string]int{}
					}
					watcher.misses[key]++
				}
				watcher.awaitingPartner, watcher.proactive = "", false
			}
			if i, name := watcher.pick(d); name != "" {
				if i >= 0 {
					watcher.remove(i)
				}
				sent := false
				if i >= 0 {
					sent = Reciprocate(d, name, allowed)
				} else {
					sent = askFirst(d, name, allowed)
				}
				if sent {
					watcher.awaitingUntil = now.Add(5 * time.Second)
					watcher.awaitingPartner = name
					watcher.proactive = i < 0
					d.stage(name, "reciprocating")
				}
			}
		}
		if !d.Delay(300 * time.Millisecond) {
			return CycleResult{}
		}
	}
	return CycleResult{}
}

// ---- best-effort banking ----

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// emergencyFleeHome/emergencyFleeDriftTiles are the mule's own EmergencyFlee reference and
// threshold — see randomevents.EmergencyFlee's own doc comment for why every caller now
// supplies its own pair instead of this package assuming every script operates near
// Catherby. mulereceiverplan.SeersStand, not Catherby: a mule's whole job keeps it planted
// at its own stand (the same physical bank tile barbarianplan.DefaultBank names), so 150
// tiles is plenty — nowhere in this script's own normal operation goes nearly that far.
var emergencyFleeHome = pathfind.NavPoint{X: mulereceiverplan.SeersStand.X, Z: mulereceiverplan.SeersStand.Z, Level: mulereceiverplan.SeersStand.Level}

const emergencyFleeDriftTiles = 150

// walkNear paths to dest and walks there, re-clicking from wherever the player stops,
// stopping once within radius tiles of dest. Ported from cmd/muletest's own walkNear
// (renamed to avoid colliding with barbarianrunner's/fisherrunner's own copies of the same
// idea — each runner package keeps its own, matching this project's existing convention of
// small per-package duplication over a shared walking helper).
func walkNear(d *Deps, dest tile.Tile, radius int) bool {
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		// EmergencyFlee here, not just in WaitForGuardian — found during a repo-wide
		// audit following tonight's incident response (same gap already fixed in every
		// sibling runner's own walk helper): this loop can hold the account mid-walk for
		// up to 20s with nothing checking combat or drift in between. Safe every tick —
		// TryLock inside EmergencyFlee already makes it a no-op whenever Guardian's
		// kick() owns the action lock instead.
		randomevents.EmergencyFlee(d.Session, d.Pack, emergencyFleeHome, emergencyFleeDriftTiles, d.Log)
		x, z, level, ok := d.Session.State().Tile()
		if ok && level == dest.Level && abs(x-dest.X) <= radius && abs(z-dest.Z) <= radius {
			return true
		}
		if !ok {
			if !d.Delay(200 * time.Millisecond) {
				return false
			}
			continue
		}
		res := d.Pack.FindPath(pathfind.NavPoint{X: x, Z: z, Level: level}, pathfind.NavPoint{X: dest.X, Z: dest.Z, Level: dest.Level})
		if !res.OK {
			d.logf("no path to %d,%d,%d: %s", dest.X, dest.Z, dest.Level, res.Reason)
			return false
		}
		if len(res.Waypoints) < 2 {
			return true
		}
		if err := actions.Walk(d.Session, res.Waypoints, false); err != nil {
			d.logf("walk: %v", err)
			return false
		}
		if !d.Delay(1500 * time.Millisecond) {
			return false
		}
	}
	return false
}

// maxBoothApproaches caps how many of a booth's own interact tiles are tried before falling
// back, so one booth standing behind a counter cannot spend the whole bank trip walking.
const maxBoothApproaches = 3

// walkToInteract puts the mule on a tile the engine will actually accept an interaction with
// target from, rather than merely one tile away from it.
//
// Why this is not walkNear(target, 1), which is what it replaces: walkNear's own arrival test is
// abs(dx) <= radius && abs(dz) <= radius, which is Chebyshev, so all four DIAGONALS of the booth
// satisfy it — and the engine's own inOperableDistance/reachRectangle1 require the two tiles to
// touch ORTHOGONALLY with no wall on the facing side. A bank booth is a blocked loc, so a click
// sent from a diagonal is accepted by the client, queued, and then never satisfies the server's
// check: no refusal, no chat line, no error, bank.IsOpen simply never becomes true and the
// caller's 20s wait burns before it tries the next booth. MuleReceiver banks after every accepted
// trade, so this path runs more often than any fisher's occasional bank trip. Same rule and same
// fix as woodcutrunner.walkToInteract and combatrunner.walkToInteract; pathfind.CanInteractFrom
// is the shared answer, kept per-package by this project's small-duplication convention.
//
// Falls back to the old walkNear(target, 1) with no pack, with no known tile, or when none of the
// interact tiles can be reached, so it is never worse than the behaviour it replaces.
func walkToInteract(d *Deps, target tile.Tile) bool {
	if d.Pack == nil {
		return walkNear(d, target, 1)
	}
	x, z, level, ok := d.Session.State().Tile()
	if !ok {
		return walkNear(d, target, 1)
	}
	here := tile.Tile{X: x, Z: z, Level: level}
	at := pathfind.NavPoint{X: target.X, Z: target.Z, Level: target.Level}
	if d.Pack.CanInteractFrom(pathfind.NavPoint{X: here.X, Z: here.Z, Level: here.Level}, at) {
		return true
	}
	stands := d.Pack.InteractTiles(at)
	if len(stands) == 0 {
		d.logf("nothing can interact with the loc at %d,%d,%d — it is walled in, or the bake has it in the wrong place", target.X, target.Z, target.Level)
		return false
	}
	sort.Slice(stands, func(i, j int) bool {
		return tile.Chebyshev(here, tile.Tile{X: stands[i].X, Z: stands[i].Z, Level: stands[i].Level}) <
			tile.Chebyshev(here, tile.Tile{X: stands[j].X, Z: stands[j].Z, Level: stands[j].Level})
	})
	// Said plainly in the log, because the diagonal case is the one worth counting: it is the one
	// that used to send a click the server ignored without saying so.
	why := "was too far away to use it"
	if here.Level == target.Level && tile.Chebyshev(here, target) <= 1 {
		why = "touched it corner to corner only, which the engine refuses"
	}
	for i, stand := range stands {
		if i >= maxBoothApproaches {
			break
		}
		if walkNear(d, tile.Tile{X: stand.X, Z: stand.Z, Level: stand.Level}, 0) {
			d.logf("walked from %d,%d onto %d,%d to use the loc at %d,%d: the tile we were on %s",
				here.X, here.Z, stand.X, stand.Z, target.X, target.Z, why)
			return true
		}
	}
	d.logf("could not reach any of the %d tile(s) that can interact with the loc at %d,%d — falling back to the old approach", len(stands), target.X, target.Z)
	return walkNear(d, target, 1)
}

// BankReceived finds the nearest bank booth to the account's current position through the
// static loc index and deposits everything in received — best-effort, exactly mirroring
// cmd/muletest's own bankReceived: gives up and logs rather than travelling far on a live
// account. Requires d.Pack, d.Locs, d.Config and d.Components all non-nil; logs and returns
// false otherwise rather than opening a bank it can't reach or read.
// depositBurstGap spaces the deposit packets of one received haul. Enough not to fire twenty
// packets inside a single 600ms game tick, small enough that sending a whole haul costs a couple
// of seconds rather than the ten the old confirm-each-before-the-next loop cost.
const depositBurstGap = 120 * time.Millisecond

func BankReceived(d *Deps, received []gamestate.SlotItem) (int, bool) {
	if len(received) == 0 {
		return 0, true
	}
	// Live-found 2026-09-16, this codebase's first real production run: every single deposit
	// below failed with "item not found in inventory" -- 0 of 26, then 0 of 27, twice in a
	// row -- because bank.Inventory() (the bank's own side-backpack read) was still reporting
	// the PRE-trade contents when this loop started, ~2s after the trade itself closed and
	// the account's pack actually changed. actions.Bank.DepositAllMatching already has a
	// fix for this exact gap (bankSideReadyWait: "the side backpack can arrive a moment after
	// the bank opens"), but that wait is scoped to the BANK OPENING, not to a just-closed
	// trade's own inventory update landing -- a different event this function's own caller
	// (mulerunner.RunCycle, right after a trade completes) had no equivalent wait for at all.
	// Poll for at least one of the actual received ids to show up before ever trying to
	// deposit any of them, capped generously (received trades can be large -- the two live
	// failures above were 26/27 distinct stacks in one screen) rather than a single fixed
	// sleep that would either be too short for a big trade or wastefully long for a small one.
	//
	// BLIND OPEN (4 Oct): the wait now reads the account's own backpack and comes BEFORE the booth
	// click, because the deposits go out with the click (openNearestBankBoothWith) and name
	// the pack slots the backpack shows.
	want := make(map[int32]bool, len(received))
	for _, it := range received {
		want[it.ID] = true
	}
	if !waitFor(d, "the received items to appear in the pack", 5*time.Second, func() bool {
		for _, it := range packItems(d) {
			if want[it.ID] {
				return true
			}
		}
		return false
	}) {
		d.logf("banking: the account's own pack still didn't show the just-received items after waiting -- depositing anyway, best effort")
	}
	pack := packItems(d)
	// The bank as this account last read it, with the bank open (every visit confirms with the bank
	// open before it closes, and nothing else changes this account's bank between visits): the
	// "before" of each deposit. Never read: no bank comparison, the pack alone confirms.
	lastBank := d.Session.State().Inventory(actions.BankWithdraw)
	// SEND EVERY DEPOSIT FIRST, THEN CONFIRM THEM ALL.
	//
	// This loop used to send one deposit and then wait up to five seconds for that one to be
	// acknowledged before sending the next. A received haul is 18-23 stacks, so even when every
	// deposit landed on the first game tick the mule spent ten seconds here -- and it trades one
	// partner at a time, so that is ten seconds of the whole fleet's hand-off capacity, once per
	// trade. Measured 2026-09-23: a 30s median cycle, 20s of it the donor's offer and 10s this.
	//
	// The deposits are ordinary packets the engine applies in order; nothing here needed them
	// serialised. Confirming afterwards is exactly as strong, because the check is a final-state
	// one -- did this id leave the pack, or did the bank's own count for it grow -- and the
	// "before" readings it compares against are all taken before any deposit is sent. The
	// per-stack diagnostics and the banked N of M count are unchanged.
	//
	// FAST BANK (4 Oct): the deposits now go out as one actions.Bank.Batch, five a tick with no
	// gap between them (they used to be spaced 120 ms apart), one Deposit-All per object kind
	// (it banks every one of the kind, wherever it sits), confirmed once. A kind the batch could
	// not confirm is sent once more the old way before the per-stack confirm below waits on it.
	type pending struct {
		it           gamestate.SlotItem
		bankID       int32
		held, before int64
		sent         bool
	}
	queue := make([]pending, 0, len(received))
	var ids []int32
	queued := map[int32]bool{}
	for _, it := range received {
		p := pending{it: it, bankID: it.ID}
		for _, item := range pack {
			if item.ID == it.ID {
				p.held += int64(item.Count)
			}
		}
		if obj, found := d.Config.Obj(int(it.ID)); found && obj.Noted() {
			p.bankID = int32(obj.CertLink)
		}
		if lastBank == nil {
			p.before = -1
		} else {
			for _, item := range lastBank.NonEmpty() {
				if item.ID == p.bankID {
					p.before += int64(item.Count)
				}
			}
		}
		if p.held == 0 {
			d.logf("deposit id=%d: %v", it.ID, actions.ErrItemNotFound)
			queue = append(queue, p)
			continue
		}
		p.sent = true
		queue = append(queue, p)
		if !queued[it.ID] {
			queued[it.ID] = true
			ids = append(ids, it.ID)
		}
	}
	bank, open, ok := openNearestBankBoothWith(d, depositOps(ids))
	if !ok {
		return 0, false
	}
	defer bank.Close()
	batch := open.Batch
	if batch.Err == nil {
		for _, sh := range batch.Short {
			if err := bank.DepositAll(sh.Op.ID); err != nil {
				d.logf("deposit id=%d (again, one at a time): %v", sh.Op.ID, err)
			}
		}
	} else if !errors.Is(batch.Err, actions.ErrBankStopped) {
		// The batch could not run at all: send them the old way, spaced, as before.
		for id := range queued {
			if err := bank.DepositAll(id); err != nil {
				d.logf("deposit id=%d: %v", id, err)
			}
			if !d.Delay(depositBurstGap) {
				break
			}
		}
	}

	deposited := 0
	for _, p := range queue {
		if !p.sent {
			continue
		}
		it, bankID, held, before := p.it, p.bankID, p.held, p.before
		// Sending a deposit is not its completion. Closing the bank immediately
		// can cancel server-queued actions, particularly when depositing notes.
		landed := func() bool {
			if before < 0 { // no earlier bank reading: the pack alone confirms
				return countByID(bank.Inventory(), it.ID) == 0
			}
			return depositLanded(bank.Inventory(), bank.Items(), it.ID, bankID, held, before)
		}
		confirmed := held > 0 && (landed() || waitFor(d, "received deposit acknowledgement", 5*time.Second, landed))
		if !confirmed && held > 0 {
			// One more reading before this is called a failure. waitFor returns false the
			// instant a stop is requested as well as on a real timeout (Delayer's contract),
			// so a runner stopped in the same second as a perfectly good deposit used to
			// report that deposit as unconfirmed. Live 2026-09-20: a mule whose bank was read
			// afterwards through a fresh open had in fact gained the coins (+1000), while its
			// own log said "deposit id=995: no confirmed inventory/bank change" and "banked 1
			// of 2" — and those counts are what the control panel and the statistics read.
			confirmed = landed()
		}
		if !confirmed {
			switch {
			case held == 0:
				d.logf("deposit id=%d: the bank's own pack view never showed the item, so there was nothing to acknowledge", it.ID)
			case !d.Delay(0):
				d.logf("deposit id=%d: a stop was requested before the bank acknowledged it — the deposit was sent and may well have landed", it.ID)
			default:
				d.logf("deposit id=%d: no confirmed inventory/bank change", it.ID)
			}
			continue
		}
		deposited++
	}
	// Tell whoever is listening what this bank now holds, from the freshest reading there is:
	// after the deposits, with the bank still open. This is the most frequent bank open the
	// mule does -- one per received haul -- so it is what keeps a published stock reading from
	// going stale between hand-outs. Reporting only; see Deps.OnBankRead.
	d.bankRead(bank.Items())
	d.logf("banked %d of %d received item stack(s)", deposited, len(received))
	return deposited, deposited == len(received)
}

// depositLanded reports whether a deposit has actually been acknowledged, from the bank's own
// two views of the account: inv is the deposit-side panel (what the pack still holds) and
// bankItems the bank's own contents. held is what the pack held before the deposit and before
// what the bank held of bankID, so before+held is what the bank should end up with.
//
// The bank having gained the units is the confirmation, and it is enough on its own. The rule
// this replaces also required the stack to have vanished from the deposit-side panel, and that
// panel is the laggier of the two readings — it is the same lag actions.bankSideReadyWait
// exists for — so a deposit plainly visible in the bank could still be reported as unconfirmed.
// A cleared pack is kept as the second, weaker signal for a bank view that has not caught up
// yet, and is trusted only while the bank view is populated: an empty one is far more likely to
// be a reading that has not arrived than a bank that genuinely holds nothing.
func depositLanded(inv, bankItems []gamestate.SlotItem, id, bankID int32, held, before int64) bool {
	gained := int64(0)
	for _, item := range bankItems {
		if item.ID == bankID {
			gained += int64(item.Count)
		}
	}
	if gained >= before+held {
		return true
	}
	if len(bankItems) == 0 {
		return false
	}
	for _, item := range inv {
		if item.ID == id {
			return false
		}
	}
	return true
}

// openNearestBankBooth finds every "Bank booth" candidate near the current tile and tries
// them nearest-first, actually opening one before giving up — the same fix already applied
// to fisherrunner.openNearestBankBooth (see its own doc comment for the live evidence: one
// specific static-bake booth tile timed out on every retry for minutes while a neighboring
// instance of the exact same booth type opened fine two tiles over). MuleReceiver banks
// after every single accepted trade, a much higher-frequency path than a fisher's occasional
// bank trip — retrying the same possibly-bad tile forever here would silently strand
// received items in the mule's own pack after every trade, not just once.
// The modal and its guardian lock share one lifetime, including cooking withdrawals.
type lockedBank struct {
	*actions.Bank
	unlock    func()
	closeOnce sync.Once
	// onClose, when set, is called with the bank's contents just before it closes: the freshest
	// reading a visit has (see Deps.OnBankOpen).
	onClose func(items []gamestate.SlotItem)
}

func (b *lockedBank) Close() error {
	var err error
	b.closeOnce.Do(func() {
		if b.onClose != nil {
			b.onClose(b.Bank.Items())
		}
		err = b.Bank.Close()
		b.unlock()
	})
	return err
}

func openNearestBankBooth(d *Deps) (*lockedBank, bool) {
	bank, _, ok := openNearestBankBoothWith(d, nil)
	return bank, ok
}

// depositOps is a Deposit-All of each id.
func depositOps(ids []int32) []actions.BankOp {
	ops := make([]actions.BankOp, 0, len(ids))
	for _, id := range ids {
		ops = append(ops, actions.DepositAllOp(id))
	}
	return ops
}

// openNearestBankBoothWith is openNearestBankBooth opening blind (actions.OpenBankBlindOps, the
// speed of MasterCrafter's six-tick gem stream): standing where the booth can be used, the booth op
// goes out as the fifth handled user event of its tick with ops right behind it (deposits naming
// the backpack's slots, withdrawals the bank's slots as the last visit's bank predicts them), so
// the engine applies them the tick after the bank opens, with no round trip between the click and
// them; the visit is then confirmed once with the bank open, and what the blind burst missed is
// sent again paced. The hooks see what they always saw: OnBankOpen the bank as this open left it
// (read with the bank open, after the ops), OnBankClose the bank as the visit closes.
func openNearestBankBoothWith(d *Deps, ops []actions.BankOp) (*lockedBank, actions.BlindOpen, bool) {
	var none actions.BlindOpen
	if d.Pack == nil || d.Locs == nil || d.Config == nil || d.Components == nil {
		d.logf("banking skipped: no pack/loc-index/config/components configured")
		return nil, none, false
	}
	if randomevents.Busy(d.Session) {
		return nil, none, false
	}
	unlock, acquired := randomevents.TryLock(d.Session)
	if !acquired {
		return nil, none, false
	}
	transferred := false
	defer func() {
		if !transferred {
			unlock()
		}
	}()
	x, z, level, ok := d.Session.State().Tile()
	if !ok {
		d.logf("banking skipped: no current tile")
		return nil, none, false
	}
	live := locindex.ChangesFrom(d.Session.State().LocChanges())
	const searchRadius = 15
	candidates := d.Locs.Find(d.Config, locindex.Query{X: x, Z: z, Level: level, Radius: searchRadius, Name: "Bank booth", Op: "Use-quickly"}, live)
	if len(candidates) == 0 {
		d.logf("banking skipped: no bank booth within %d tiles", searchRadius)
		return nil, none, false
	}
	const maxTries = 4
	for i, booth := range candidates {
		if i >= maxTries {
			d.logf("banking skipped: gave up after %d bank booth candidates", maxTries)
			return nil, none, false
		}
		if !walkToInteract(d, tile.Tile{X: booth.X, Z: booth.Z, Level: booth.Level}) {
			continue
		}
		// The open hands the bank over the moment the open's own inventories have arrived
		// (actions.Bank.AwaitOpen: not after the fixed 500 ms pause this used to take), so
		// OnBankOpen below publishes this open's reading, never the last visit's.
		o := actions.OpenBankBlindOps(d.Session, d.Components, actions.BankBooth{Op: booth.Op, X: booth.X, Z: booth.Z, LocID: booth.ID}, ops, handoutWaiter{d}, 20*time.Second)
		if o.Bank == nil {
			if !d.Delay(0) || errors.Is(o.Batch.Err, actions.ErrBankStopped) {
				return nil, none, false
			}
			if o.Batch.Err != nil && !errors.Is(o.Batch.Err, actions.ErrNotOnScreen) {
				d.logf("OpLoc on booth: %v", o.Batch.Err)
			} else {
				d.logf("timed out waiting for bank to open")
			}
			continue
		}
		if errors.Is(o.Batch.Err, actions.ErrBankStopped) {
			_ = o.Bank.Close()
			return nil, none, false
		}
		if !o.Fresh {
			d.logf("banking: the bank opened but its inventories did not refresh; using them as they are")
		}
		if len(ops) > 0 && (!o.Blind || !o.Batch.OK()) {
			d.logf("banking: %s", o)
		}
		transferred = true
		d.bankOpened(o.Bank.Items())
		return &lockedBank{Bank: o.Bank, unlock: unlock, onClose: d.bankClosing}, o, true
	}
	return nil, none, false
}

// DepositAllInPack opens the nearest bank and deposits every stack the mule is currently
// holding, keyed by id so a stack spread across several slots (e.g. unstackable raw fish)
// still goes in one DepositAll per id. Account owner's own ask (2026-09-16): the mule should
// go into every trade with an empty pack, not just clean up once at startup — called from
// muleBot.Loop before each RunCycle whenever the pack isn't already empty (cheap to check:
// this returns immediately with no walk at all when there is nothing to deposit). Covers
// the startup case for free, too — a mule nudged around with a temporary fishing script
// (the account owner's own real workaround for getting a mule physically to Catherby before
// MuleReceiver could do it itself) can arrive holding real leftover junk (a fishing net, a
// handful of raw shrimp) that BankReceived's own per-trade deposit never touches, since that
// only ever banks what a specific trade JUST received — this is checked before the very
// first trade too, not only between later ones.
func DepositAllInPack(d *Deps) (int, bool) {
	if d.Components == nil {
		return 0, false
	}
	// PlayerPack (3214), not actions.NewEquipment(d.Session, d.Components).Inventory() —
	// real, live-found bug (2026-09-16, chasing why a cook round's own end-of-round
	// deposit reported "banked 0 stack(s)" while the account's pack genuinely still held
	// the just-withdrawn, un-cooked raw fish): after a bank-open/close, a real door
	// crossing and a multi-tile walk (exactly what a cook round does, and plausibly other
	// sequences too), Equipment.Inventory()'s own dynamic SideIcon-based tab resolution
	// returned ZERO entries here while PlayerPack's fixed component id read the real held
	// items correctly — see PlayerPack's own doc comment (cook.go) for the fuller account
	// and cmd/cooktest for the live reproduction. Using the same fixed id this package's
	// own cook.go already switched to for the identical reason, rather than leaving this
	// call site on the fragile path.
	rows := packItems(d)
	if len(rows) == 0 {
		return 0, true
	}
	seen := map[int32]bool{}
	var ids []int32
	for _, it := range rows {
		if !seen[it.ID] {
			seen[it.ID] = true
			ids = append(ids, it.ID)
		}
	}
	// Opened blind: the Deposit-Alls ride behind the booth click and are banked the tick after
	// the bank opens; depositKindsFast then has only what the blind burst missed left to send.
	bank, _, ok := openNearestBankBoothWith(d, depositOps(ids))
	if !ok {
		return 0, false
	}
	defer bank.Close()
	return depositKindsFast(d, bank, ids), true
}

// depositKindsFast banks every one of each id through an open bank: one Deposit-All per kind,
// all sent together five a tick and confirmed once against the pack (actions.Bank.Batch). A kind
// the batch could not confirm goes the old way, one at a time, waiting for the pack to lose it.
// It returns how many kinds left the pack.
func depositKindsFast(d *Deps, bank *lockedBank, ids []int32) int {
	if len(ids) == 0 {
		return 0
	}
	ops := make([]actions.BankOp, 0, len(ids))
	for _, id := range ids {
		ops = append(ops, actions.DepositAllOp(id))
	}
	r := bank.Batch(ops, handoutWaiter{d}, actions.BatchOptions{})
	if errors.Is(r.Err, actions.ErrBankStopped) {
		return 0
	}
	if !r.OK() {
		d.logf("banking: %s — the rest one at a time", r)
	}
	deposited := 0
	for _, id := range ids {
		if r.Err == nil {
			if _, short := r.ShortOf(actions.BankOpDeposit, id); !short {
				deposited++
				continue
			}
		}
		if depositOneSlow(d, bank, id) {
			deposited++
		}
	}
	return deposited
}

// depositOneSlow is the old one-at-a-time Deposit-All of id, waiting for the pack to lose it.
//
// Live-found 2026-09-16, this codebase's first real production run: the very first version of
// DepositAllInPack called DepositAll for each id back-to-back with no wait at all, then closed
// the bank on defer — a real click was sent and returned no error every time, yet the items were
// still sitting in the pack afterward. deposit() only confirms the CLICK was sent, not that the
// server applied it; this waits for the pack to lose the item. The fast batch above sends back
// to back too, but it confirms against the pack before anything closes the bank, and it only
// runs on a bank whose open sent fresh inventories (openNearestBankBooth's AwaitOpen), so a
// click on a slot the server no longer holds there is reported short and lands here.
func depositOneSlow(d *Deps, bank *lockedBank, id int32) bool {
	if err := bank.DepositAll(id); err != nil {
		d.logf("deposit id=%d: %v", id, err)
		return false
	}
	return waitFor(d, fmt.Sprintf("pack to lose id=%d after deposit", id), 5*time.Second, func() bool {
		for _, p := range bank.Inventory() {
			if p.ID == id {
				return false
			}
		}
		return true
	})
}
