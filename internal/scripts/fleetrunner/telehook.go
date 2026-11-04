package fleetrunner

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/randomevents"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
)

const (
	// teleExpectTTL is how long a cast's landing is announced to the Guardian: the cast takes about four ticks.
	teleExpectTTL = 15 * time.Second
	// castWaitTicks is how many ticks a travel teleport has to move the account (the old travelCastWait, six seconds).
	castWaitTicks = 10
	// landMemoTTL is how long a landing's price is remembered, landMemoFailTTL a landing that had no route (shorter, so an
	// account that has just earned its fares is not locked out), landMemoMax the most prices kept.
	landMemoTTL     = 10 * time.Minute
	landMemoFailTTL = 2 * time.Minute
	landMemoMax     = 96
	// teleSilentPause and teleSilentPauseLong pause a spell after a cast that did not move the account and said nothing, the
	// second after teleSilentStrikes in a row; teleWildernessPause a spell the Wilderness refused.
	teleSilentPause     = 2 * time.Minute
	teleSilentPauseLong = 10 * time.Minute
	teleSilentStrikes   = 3
	teleWildernessPause = 5 * time.Minute
	// castFailedFor is how long after a cast that moved nothing the fare gate promises no cast.
	castFailedFor = 10 * time.Minute
	// teleMissedEvery spaces the missed-saving entries an account adds, so one walk's several decisions are one entry.
	teleMissedEvery = 2 * time.Minute
	// guardianWaitBound bounds the wait for a random event on a Walker that has no collision pack to flee with.
	guardianWaitBound = 90 * time.Second
)

// fleetTele is the casts the whole process made in the last hour: every account shares one process, so this, not the per-account
// cap, is the brake on a law-drain bug (scarabplan.TeleTravelSettings.FleetPerHour).
var fleetTele struct {
	mu sync.Mutex
	w  fleetplan.SlidingCount
}

func FleetTeleCount(now time.Time) int {
	fleetTele.mu.Lock()
	defer fleetTele.mu.Unlock()
	return fleetTele.w.Count(now, time.Hour)
}

func fleetTeleAdd(now time.Time) {
	fleetTele.mu.Lock()
	fleetTele.w.Add(now)
	fleetTele.mu.Unlock()
}

// landKey identifies a priced landing: where it is, where the walk is bound, everything else that decides the price
// (webwalk.FollowQuery.PlanKey) and how much the account can pay in fares (a band of its coins).
type landKey struct {
	land, dest tile.Tile
	plan       uint64
	coins      int
}

type landEntry struct {
	facts fleetplan.RouteFacts
	at    time.Time
}

// walkEstimate is what a cast predicted the landing's walk would take, to compare with what it took.
type walkEstimate struct {
	on        bool
	TickLen   int
	predicted int
	dest      tile.Tile
	spell     string
}

// InstallTeleportHook gives w the teleport decision on every walk it makes (webwalk.Walker.BeforeFollow), whoever calls it. It
// sets nothing else (Prepare, which the run staller and RuneRunner's composed stall use, is left alone). deps builds the Deps the
// hook reads the account through, once per decision; it MUST carry Pack (the Guardian's flee needs the collision pack). Nothing
// for a nil Travel or one with teleports off.
func (t *Travel) InstallTeleportHook(w *webwalk.Walker, deps func() *Deps) {
	if t == nil || w == nil || deps == nil || !t.Tele.Enabled {
		return
	}
	w.BeforeFollow = t.TeleportHook(deps)
}

// TeleportHook is the function Walker.BeforeFollow runs.
func (t *Travel) TeleportHook(deps func() *Deps) func(webwalk.FollowQuery) webwalk.BeforeFollowResult {
	if t == nil || deps == nil {
		return nil
	}
	return func(q webwalk.FollowQuery) webwalk.BeforeFollowResult {
		if !t.Tele.Enabled {
			return webwalk.BeforeFollowResult{}
		}
		d := deps()
		if d == nil || d.Session == nil || d.Config == nil {
			return webwalk.BeforeFollowResult{}
		}
		return t.decideAndCast(d, q)
	}
}

// PreviewCast is the decision for a walk that has not started and may not go through scarabrunner.WalkTo (a quest leg's
// BeforeTravel): it previews the walk to dest through w, decides and casts exactly as the hook would, and reports whether the
// account moved. The Walker's own hook then finds the account beside the landing and declines, so there is no second cast.
//
// It plans the walk from here once to price it, so it asks only when a cast could be worth it: the legs a straight run of
// which is under a cast and the margin (about sixty tiles) are left to the Walker's own hook, and so are the ones the settings
// forbid. The options are the quest engine's own (none), so the landings it prices are remembered for the walk that follows.
func (t *Travel) PreviewCast(d *Deps, w *webwalk.Walker, dest tile.Tile) bool {
	if t == nil || !t.Tele.Enabled || w == nil || d == nil || d.Session == nil || d.Config == nil {
		return false
	}
	tele := t.Tele.Normalised()
	if (!tele.AnyWalk && !t.muleWalk) || tele.FleetPerHour < 0 || MagicLevel(d) < fleetplan.LowestTeleportLevel() {
		return false
	}
	here := CurrentTile(d)
	if here == nil || (tile.Chebyshev(tile.Tile{X: here.X, Z: here.Z}, tile.Tile{X: dest.X, Z: dest.Z})+1)/2 < tele.CastTicks+tele.MinSaveTicks {
		return false
	}
	q, ok := w.PreviewFollow(webwalk.Point{X: dest.X, Z: dest.Z, Level: dest.Level}, webwalk.WalkOptions{})
	if !ok {
		return false
	}
	return t.decideAndCast(d, q).Moved
}

// CastFailedRecently is whether a cast in the last ten minutes did not move the account: the fare gate promises nothing on a
// cast that has just failed (scarabplan.FareCoverInput.CastFailedRecently), so a refused cast cannot leave the account on the
// wrong coast with no coins.
func (t *Travel) CastFailedRecently() bool {
	return t != nil && !t.castFailedAt.IsZero() && t.now().Sub(t.castFailedAt) < castFailedFor
}

// BlockedFunc is the blocked-spell test of a decision made now: the spells paused or refused, as the hook sees them.
func (t *Travel) BlockedFunc(d *Deps) func(string) bool {
	if t == nil {
		return func(string) bool { return false }
	}
	return t.blockedNow(d, PackRunes(d))
}

// routeFacts reads a planned route into what the decision prices. A route that failed, or that ends where the walk starts (the
// nearest reachable tile), is no route.
func routeFacts(r webwalk.Route, from webwalk.Point) fleetplan.RouteFacts {
	if !r.OK || len(r.Waypoints) == 0 || r.End() == from {
		return fleetplan.RouteFacts{}
	}
	hops, ships := 0, 0
	for _, h := range r.Hops() {
		hops += h.Cost
		if h.Kind == webwalk.KindShip {
			ships++
		}
	}
	return fleetplan.RouteFacts{OK: true, TileUnits: max(r.Cost-hops, 0), HopUnits: hops, Ships: ships}
}

// coinBand is the account's coins in the bands the fare edges care about (a ship costs about thirty).
func coinBand(coins int) int {
	switch {
	case coins < 30:
		return 0
	case coins < 100:
		return 1
	case coins < fleetplan.FareFloat:
		return 2
	}
	return 3
}

// landCost is DecideTeleport's LandCost: the walk from a landing, from the memo when it is fresh, else one planner run.
func (t *Travel) landCost(d *Deps, q webwalk.FollowQuery, dest tile.Tile, runs *int, priced *[]string) func(tile.Tile) fleetplan.RouteFacts {
	var plan uint64
	if q.PlanKey != nil {
		plan = q.PlanKey()
	}
	coins := coinBand(CoinsHeld(d))
	return func(land tile.Tile) fleetplan.RouteFacts {
		key := landKey{land: land, dest: dest, plan: plan, coins: coins}
		now := t.now()
		if e, ok := t.landMemo[key]; ok {
			ttl := landMemoTTL
			if !e.facts.OK {
				ttl = landMemoFailTTL
			}
			if now.Sub(e.at) < ttl {
				return e.facts
			}
		}
		from := webwalk.Point{X: land.X, Z: land.Z}
		facts := routeFacts(q.PlanFrom(from), from)
		*runs++
		*priced = append(*priced, fmt.Sprintf("%d,%d", land.X, land.Z))
		if t.landMemo == nil || len(t.landMemo) >= landMemoMax {
			for k, e := range t.landMemo {
				if now.Sub(e.at) >= landMemoTTL {
					delete(t.landMemo, k)
				}
			}
			if t.landMemo == nil || len(t.landMemo) >= landMemoMax {
				t.landMemo = map[landKey]landEntry{}
			}
		}
		t.landMemo[key] = landEntry{facts: facts, at: now}
		return facts
	}
}

// decideAndCast is one BeforeFollow decision.
func (t *Travel) decideAndCast(d *Deps, q webwalk.FollowQuery) (res webwalk.BeforeFollowResult) {
	tele := t.Tele.Normalised()
	// The cheap gates first: none of these reads the pack, the quest tab or the planner, and most walks end here.
	if !tele.AnyWalk && !t.muleWalk {
		return res // the 6 Oct rollback: only the walk to the mule's post
	}
	if tele.FleetPerHour < 0 {
		return res
	}
	if q.Route.OK && q.Route.Cost/2 < tele.CastTicks+tele.MinSaveTicks {
		return res // not even a straight run of it is worth a cast
	}
	if MagicLevel(d) < fleetplan.LowestTeleportLevel() {
		return res
	}
	here := tile.Tile{X: q.Here.X, Z: q.Here.Z, Level: q.Here.Level}
	dest := tile.Tile{X: q.Dest.X, Z: q.Dest.Z, Level: q.Dest.Level}
	st := d.Session.State()
	now := t.now()

	staff, worn := StaffOnHand(d)
	if !worn {
		staff = "" // the spell needs the staff on, not merely owned
	}
	var carrying []string
	for _, it := range PackItems(d) {
		carrying = append(carrying, it.Name)
	}
	pack := PackRunes(d)
	energy, energyKnown := st.Energy()
	kg, _ := st.Weight()
	stalls, _ := t.StallMode(d)
	_, pocket := randomevents.PocketAt(here.X, here.Z, here.Level)
	_, strand := randomevents.StrandOwner(here.X, here.Z, here.Level)
	laws, lawsKnown := 0, false
	if t.MuleLaws != nil {
		laws, lawsKnown = t.MuleLaws()
	}
	runs := 0
	var priced []string
	in := fleetplan.TeleDecisionInput{
		Here: here, Dest: dest, HereRoute: routeFacts(q.Route, q.Here),
		LandCost: t.landCost(d, q, dest, &runs, &priced),
		Magic:    MagicLevel(d), Members: t.Members, Pack: pack, Staff: staff,
		QuestDone: questDone(d), Blocked: t.blockedNow(d, pack), Carrying: carrying,
		InCombat: st.InCombat(), PocketOrStrand: pocket || strand, MuleWalk: t.muleWalk,
		Energy: energy, EnergyKnown: energyKnown, KG: kg, Stalled: stalls,
		LastCast: t.lastTele, Now: now,
		CastsLastHour: t.teleCasts.Count(now, time.Hour), FleetCastsLastHour: FleetTeleCount(now),
		MuleLaws: laws, MuleKnown: lawsKnown,
	}
	dec, why := fleetplan.DecideTeleport(t.Tele, in)
	res.Planned = runs > 0
	if runs > 0 {
		t.sayEvery(d, "tele-priced", time.Minute, fmt.Sprintf("teleport travel: priced %d landing(s) (%s) for the walk from %d,%d to %d,%d", runs, strings.Join(priced, "; "), here.X, here.Z, dest.X, dest.Z))
	}
	if why != "" {
		if dec.MissedSave > 0 && now.Sub(t.missedAt) >= teleMissedEvery {
			t.missedAt = now
			t.Missed.Add(now, dec.MissedSave)
		}
		t.sayDecline(d, here, dest, why, dec)
		return res
	}
	// A random event is present: its handler owns the account; do not hold the walk behind it, decline this decision.
	if randomevents.Busy(d.Session) {
		t.sayDecline(d, here, dest, fleetplan.DeclineEvent, dec)
		return res
	}
	res.Planned = true
	res.Moved = t.castTravel(d, dec, here, dest, energy, energyKnown)
	return res
}

// sayDecline logs a decline once per ten minutes per reason (the settings' own declines need no remark).
func (t *Travel) sayDecline(d *Deps, here, dest tile.Tile, why string, dec fleetplan.TeleDecision) {
	if fleetplan.QuietDecline(why) {
		return
	}
	detail := ""
	if dec.Detail != "" {
		detail = ": " + dec.Detail
	}
	t.sayOnce(d, "tele-declined:"+why, fmt.Sprintf("teleport travel: declined (%s) from %d,%d toward %d,%d%s", why, here.X, here.Z, dest.X, dest.Z, detail))
}

// castTravel casts dec's teleport and reports whether the account moved. A cast that does not move it is answered by what the
// server said (classifyRefusal).
func (t *Travel) castTravel(d *Deps, dec fleetplan.TeleDecision, here, dest tile.Tile, energy int, energyKnown bool) bool {
	tele := t.Tele.Normalised()
	spell := dec.Teleport.Spell.Name
	land := dec.Landing()
	t.CloseRubMenu(d)
	for attempt := 0; attempt < 2; attempt++ {
		if !t.settleBeforeSpell(d, tele.SettleTicks) {
			return false
		}
		if attempt > 0 && !waitOutEvent(d) {
			return false
		}
		cursor := chatCursor(d)
		randomevents.ExpectTeleport(d.Session, land.X, land.Z, teleExpectTTL)
		if err := actions.Teleport(d.Session, spell); err != nil {
			randomevents.ForgetTeleports(d.Session)
			t.block(spell)
			t.castFailedAt = t.now()
			d.Logf("teleport travel: %s: %v; walking", spell, err)
			return false
		}
		moved, stopped := t.waitLanding(d, land)
		if stopped {
			return false
		}
		if moved {
			t.noteCast(d, dec, here, dest, energy, energyKnown)
			return true
		}
		lines := chatLinesSince(d, cursor)
		class, rune := classifyRefusal(lines, randomevents.Busy(d.Session))
		quiet := class == refusedEvent || class == refusedUnknown
		if quiet && attempt == 0 {
			// The cast's turn was taken by an AFK random event (the flag is consumed, the next roll is five minutes away), or the
			// click was dropped while the player was delayed: settle, let the Guardian deal with the event, and cast once more.
			d.Logf("teleport travel: %s from %s did not land (%s: %s)", spell, HereIs(d), class, class.retry())
			continue
		}
		t.castFailedAt = t.now()
		t.answerRefusal(d, spell, class, rune, lines)
		return false
	}
	return false
}

// settleBeforeSpell waits until the spell click will be read: SettleTicks since the account's own last alchemy cast or rub, and
// the cast's outcome settled. A delayed player drops the click with no message, and a walk alch cast delays it for four ticks.
func (t *Travel) settleBeforeSpell(d *Deps, settleTicks int) bool {
	need := time.Duration(settleTicks) * t.TickLen()
	for i := 0; i < 24; i++ {
		now := t.now()
		last := t.lastCast
		if t.lastRub.After(last) {
			last = t.lastRub
		}
		since := now.Sub(last)
		if t.pending {
			t.settleCast(d, now)
		}
		if since >= need && !t.pending {
			return true
		}
		wait := max(need-since, t.TickLen())
		if !d.Delay(wait) {
			return false
		}
	}
	return true
}

// waitLanding waits up to castWaitTicks ticks for the account to stand within four tiles of land. stopped says the run was
// stopped while it waited, which is neither a landing nor a refusal.
func (t *Travel) waitLanding(d *Deps, land tile.Tile) (moved, stopped bool) {
	near := func() bool {
		cur := CurrentTile(d)
		return cur != nil && tile.Chebyshev(tile.Tile{X: cur.X, Z: cur.Z}, land) <= 4
	}
	for i := 0; i < castWaitTicks; i++ {
		if near() {
			return true, false
		}
		if !d.Delay(t.TickLen()) {
			return false, true
		}
	}
	return near(), false
}

// waitOutEvent lets the Guardian deal with a random event before a recast. The wait is the runner's own (WaitForGuardian, which
// also flees an attacker) when the Deps carry the collision pack; without one it is bounded.
func waitOutEvent(d *Deps) bool {
	if !randomevents.Busy(d.Session) {
		return true
	}
	if d.Pack != nil {
		return WaitForGuardian(d)
	}
	deadline := time.Now().Add(guardianWaitBound)
	for randomevents.Busy(d.Session) && time.Now().Before(deadline) {
		if !d.Delay(500 * time.Millisecond) {
			return false
		}
	}
	return true
}

// noteCast records a cast that landed.
func (t *Travel) noteCast(d *Deps, dec fleetplan.TeleDecision, here, dest tile.Tile, energy int, energyKnown bool) {
	now := t.now()
	spell := dec.Teleport.Spell.Name
	land := dec.Landing()
	t.mu.Lock()
	t.Teleports++
	t.Saved += dec.Save
	t.mu.Unlock()
	t.lastTele = fleetplan.TeleCast{Spell: spell, Landing: land, At: now}
	t.castFailedAt = time.Time{}
	t.teleCasts.Add(now)
	fleetTeleAdd(now)
	delete(t.teleStrikes, spell)
	// The jump is not a step of the walk: the next poll's previous tile must not be the tile before the cast, or the
	// walk log counts the 300 tiles as walked and the zone trigger scan runs across the whole map.
	t.walkPrevOK = false
	t.est = walkEstimate{on: true, TickLen: d.Session.State().Tick(), predicted: dec.LandTicks, dest: dest, spell: spell}
	e := "?"
	if energyKnown {
		e = fmt.Sprintf("%d%%", energy)
	}
	d.Logf("teleport travel: %s from %d,%d toward %d,%d: saves %d ticks (walk %d, cast %d, landing walk %d, energy %s)",
		spell, here.X, here.Z, dest.X, dest.Z, dec.Save, dec.HereTicks, t.Tele.Normalised().CastTicks, dec.LandTicks, e)
	d.Delay(t.TickLen()) // the cast's last tick (anim reset) before the first walk click
}

// logEstimate says how long the walk from the landing took against what the cast predicted, once, when the walk ends.
func (t *Travel) logEstimate(d *Deps) {
	if t == nil || !t.est.on {
		return
	}
	e := t.est
	t.est = walkEstimate{}
	st := d.Session.State()
	took := st.Tick() - e.TickLen
	away := -1
	if here := CurrentTile(d); here != nil {
		away = tile.Chebyshev(tile.Tile{X: here.X, Z: here.Z}, e.dest)
	}
	if took <= 0 || took > 4000 {
		return // the engine tick restarted, or this is some later, unrelated walk
	}
	d.Logf("teleport travel: walk estimate: after %s the walk to %d,%d was predicted at %d ticks and took %d (ended %d tiles from it)",
		e.spell, e.dest.X, e.dest.Z, e.predicted, took, away)
}

// refusal is what a cast that did not move the account was refused for.
type refusal int

const (
	refusedUnknown refusal = iota // no message and no event: the click was dropped, or an event with no NPC yet took the turn
	refusedEvent                  // a random event is present, or the server said something we do not know
	refusedMembers
	refusedLevel
	refusedQuest
	refusedRunes
	refusedWilderness
)

func (r refusal) String() string {
	switch r {
	case refusedEvent:
		return "afk event"
	case refusedMembers:
		return "members"
	case refusedLevel:
		return "level"
	case refusedQuest:
		return "quest"
	case refusedRunes:
		return "runes"
	case refusedWilderness:
		return "wilderness"
	}
	return "silent drop"
}

// retry is what the retry of a quiet class does, for the log.
func (r refusal) retry() string {
	if r == refusedEvent {
		return "one recast"
	}
	return "settled and recast"
}

var (
	questRefusalLine      = regexp.MustCompile(`(?i)you haven't learnt|must have completed|need to have completed`)
	wildernessRefusalLine = regexp.MustCompile(`(?i)mysterious force`)
	runeNameLine          = regexp.MustCompile(`(?i)you do not have enough (.*?)\s*runes?\b`)
)

// classifyRefusal reads the game messages that followed a cast that did not move the account (teleport.rs2 and magic.rs2's
// check_spell_requirements, via the helpers actions already has): members, level, runes (with the rune named), the quests'
// scrolls and the Wilderness line are refusals that will not change in minutes. A message nobody here knows, or a random event
// standing in view, is an AFK event (they spawn with no message of their own and take the cast, runes untouched); neither
// message nor event is a silent drop. Both of those are retried, never blocked for an hour.
func classifyRefusal(lines []string, eventPresent bool) (refusal, string) {
	heard := false
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		heard = true
		switch {
		case actions.IsMembersSpellLine(l):
			return refusedMembers, ""
		case actions.IsSpellLevelLine(l):
			return refusedLevel, ""
		case actions.IsNoRunesLine(l):
			name := "Law"
			if m := runeNameLine.FindStringSubmatch(l); m != nil && strings.TrimSpace(m[1]) != "" {
				name = strings.TrimSpace(m[1])
			}
			return refusedRunes, strings.ToUpper(name[:1]) + strings.ToLower(name[1:]) + " rune"
		case questRefusalLine.MatchString(l):
			return refusedQuest, ""
		case wildernessRefusalLine.MatchString(l):
			return refusedWilderness, ""
		}
	}
	if eventPresent || heard {
		return refusedEvent, ""
	}
	return refusedUnknown, ""
}

// answerRefusal blocks the spell for as long as its refusal will hold, and says so.
func (t *Travel) answerRefusal(d *Deps, spell string, class refusal, runeName string, lines []string) {
	said := strings.TrimSpace(strings.Join(lines, " "))
	if said == "" {
		said = "no message"
	}
	here := HereIs(d)
	switch class {
	case refusedMembers, refusedQuest:
		t.block(spell)
		d.Logf("teleport travel: %s from %s did not land (%s: %s); not casting it for %s, walking", spell, here, class, said, fleetplan.TeleportRefusalBlocks)
	case refusedLevel:
		if t.teleBlockLevel == nil {
			t.teleBlockLevel = map[string]int{}
		}
		t.teleBlockLevel[spell] = MagicLevel(d)
		d.Logf("teleport travel: %s from %s did not land (%s: %s); not casting it until Magic rises, walking", spell, here, class, said)
	case refusedRunes:
		if t.teleBlockRunes == nil {
			t.teleBlockRunes = map[string]runeBlock{}
		}
		t.teleBlockRunes[spell] = runeBlock{rune: runeName, held: PackRunes(d)[runeName]}
		d.Logf("teleport travel: %s from %s did not land (%s: %s); not casting it until the pack holds more %s, walking", spell, here, class, said, runeName)
	case refusedWilderness:
		t.blockFor(spell, teleWildernessPause)
		d.Logf("teleport travel: %s from %s did not land (%s: %s); not casting it for %s, walking", spell, here, class, said, teleWildernessPause)
	default:
		// An AFK event or a dropped click that came twice: a short pause, longer when it keeps happening, never an hour.
		if t.teleStrikes == nil {
			t.teleStrikes = map[string]int{}
		}
		t.teleStrikes[spell]++
		pause := teleSilentPause
		if t.teleStrikes[spell] >= teleSilentStrikes {
			pause = teleSilentPauseLong
		}
		t.blockFor(spell, pause)
		d.Logf("teleport travel: %s from %s did not land twice (%s: %s); pausing it for %s, walking", spell, here, class, said, pause)
	}
}

// runeBlock is a spell refused for want of a rune: blocked until the pack holds more of it than it did.
type runeBlock struct {
	rune string
	held int
}

// blockFor blocks spell for dur.
func (t *Travel) blockFor(spell string, dur time.Duration) {
	if t.teleBlocked == nil {
		t.teleBlocked = map[string]time.Time{}
	}
	t.teleBlocked[spell] = t.now().Add(dur)
}

// blockedNow is the Blocked func of a decision: the time blocks (blocked), the spells refused for a level the account has not
// reached since, and the ones refused for a rune the pack still holds no more of.
func (t *Travel) blockedNow(d *Deps, pack fleetplan.RuneStock) func(string) bool {
	magic := MagicLevel(d)
	return func(spell string) bool {
		if t.blocked(spell) {
			return true
		}
		if lvl, ok := t.teleBlockLevel[spell]; ok {
			if magic > lvl {
				delete(t.teleBlockLevel, spell)
			} else {
				return true
			}
		}
		if rb, ok := t.teleBlockRunes[spell]; ok {
			if pack[rb.rune] > rb.held {
				delete(t.teleBlockRunes, spell)
			} else {
				return true
			}
		}
		return false
	}
}

// chatCursor is the chat log's line count now, so the lines a cast brings can be read back without the clock.
func chatCursor(d *Deps) int {
	if d.Chat == nil {
		return 0
	}
	return d.Chat.Total()
}

// chatLinesSince is the game messages that arrived after cursor.
func chatLinesSince(d *Deps, cursor int) []string {
	if d.Chat == nil {
		return nil
	}
	lines, _ := d.Chat.ReadAfter(cursor, time.Time{})
	return lines
}

// MissedTicks is the walking, in ticks, that casts the account could not pay for would have saved in the last three hours.
func (t *Travel) MissedTicks() int {
	if t == nil {
		return 0
	}
	return t.Missed.Total(t.now())
}

// ClearMissed forgets the missed savings (a kit ask was made for them).
func (t *Travel) ClearMissed() {
	if t != nil {
		t.Missed.Clear()
	}
}
