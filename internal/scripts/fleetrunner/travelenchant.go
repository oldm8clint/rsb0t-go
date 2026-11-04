package fleetrunner

// The walk enchant (a user request: "ring enchanting should be done while walking instead of while standing at
// the furnace"): MasterCrafter's jewellery round arms its Travel with the enchant of its batch before a walk that
// carries the made pieces, and the walk's own poll casts it on them. The decision is scarabplan/walkenchant.go; the
// machinery is alchPoll's (a cast only a clear tick after the walk's click, only with the click's target far enough
// ahead, a clear-move in the write in front of it so a zone trigger cannot halt the walk), with three differences:
//
//   - the cast is confirmed by the enchanted piece's count rising in the pack (the spell replaces the piece in its
//     own slot), not by Magic experience alone, and the next is sent when two more client ticks have been seen after
//     it (the spell's p_delay(1): the engine takes one every third tick). A cast that did not show in four ticks is
//     taken as dropped, widens the gap by a tick, and three in a row end the walk's enchanting;
//   - a refusal in the chat (level, runes, members, the wrong item) ends it at once and is handed back, so the
//     round stops asking for enchants it will not get;
//   - the bot takes the result after the walk (EnchantWalkTake) and counts what landed against its ledger: nothing
//     here knows of a batch.
//
// CPU: no goroutine and no timer. A poll that is not due costs a few comparisons; a due one reads the pack once.

import (
	"time"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
)

// EnchantWalk names the enchant a walk casts: the spell by its book name, the piece it is cast on and the piece
// that results (obj ids, not names: the jewellery rows are known by id).
type EnchantWalk struct {
	Spell           string
	BaseID, FinalID int32
	// Refusal reads the chat since the cast for the engine's refusals ("level", "runes", "members", "item"; ""
	// for none). Nil reads none.
	Refusal func(said string) string
}

// EnchantWalkResult is what a walk's enchanting did.
type EnchantWalkResult struct {
	// Sent is the casts sent, Landed the ones seen to land, XP the Magic experience gained meanwhile in displayed
	// points, Ticks the server ticks the walk spanned, Left the pieces still in the pack.
	Sent, Landed, XP, Ticks, Left int
	// Reason is "" while all went well and there were pieces left, "done" when no piece was left, a refusal
	// ("level", "runes", "members", "item") or "stalled" (three casts in a row that never showed).
	Reason string
}

// enchantWalkLandTicks is how many ticks a cast has to show its piece before it is taken as dropped
// (mastercraftrunner's enchantLand).
const enchantWalkLandTicks = 4

// enchantWalkState is the walk enchant in progress.
type enchantWalkState struct {
	on, done bool
	cfg      EnchantWalk
	// startedAt bounds the chat a refusal is read from; tick0 and xp0 are the readings at the start.
	startedAt time.Time
	tick0     int
	xp0       int
	// pending is a cast sent and not yet seen to land: sentAt when, before the enchanted pieces then held.
	pending bool
	sentAt  time.Time
	before  int
	// landedTick is the client tick the last cast was seen to land on (-1 for none); gap the ticks that must pass
	// after it, misses the casts in a row that never showed.
	landedTick, gap, misses int
	sent, landed            int
	reason                  string
}

// ArmEnchant sets the next walks to cast e on the pieces in the pack; the walk after it counts from here.
// EnchantWalkTake ends it. Safe on a nil Travel.
func (t *Travel) ArmEnchant(d *Deps, e EnchantWalk) {
	if t == nil || e.Spell == "" || e.BaseID == 0 || e.FinalID == 0 {
		return
	}
	t.ench = enchantWalkState{
		on: true, cfg: e, startedAt: t.now(), tick0: d.Session.State().Tick(), xp0: MagicXP(d),
		landedTick: -1, gap: fleetplan.WalkEnchantGapTicks,
	}
}

// EnchantArmed is whether a walk enchant is armed and not yet taken. Safe on a nil Travel.
func (t *Travel) EnchantArmed() bool { return t != nil && t.ench.on }

// enchantActive is whether the walk casts an enchant now.
func (t *Travel) enchantActive() bool { return t != nil && t.ench.on && !t.ench.done }

// enchantPieces is the pack pieces and the first slot of the base piece, and how many of it the pack holds.
func (t *Travel) enchantPieces(d *Deps) (pack []gamestate.SlotItem, first gamestate.SlotItem, pieces int) {
	pack = PackSlotItems(d)
	for _, it := range pack {
		if it.ID == t.ench.cfg.BaseID {
			if pieces == 0 {
				first = it
			}
			pieces += max(int(it.Count), 1)
		}
	}
	return pack, first, pieces
}

// enchantSeen settles the cast in flight: it landed (the enchanted piece's count rose), was refused (the chat),
// was dropped (it did not show in enchantWalkLandTicks), or is still on its way. It reports the last.
func (t *Travel) enchantSeen(d *Deps, now time.Time, tick int) (stillPending bool) {
	e := &t.ench
	if !e.pending {
		return false
	}
	if n := PackCountOf(d, int(e.cfg.FinalID)); n > e.before {
		e.landed += n - e.before
		e.landedTick, e.pending, e.misses = tick, false, 0
		return false
	}
	if e.cfg.Refusal != nil {
		if why := e.cfg.Refusal(ChatSince(d, e.startedAt)); why != "" {
			e.pending, e.done, e.reason = false, true, why
			return false
		}
	}
	if now.Sub(e.sentAt) >= enchantWalkLandTicks*t.TickLen() {
		e.pending = false
		e.misses++
		e.gap = min(e.gap+1, fleetplan.WalkEnchantGapTicks+3)
		e.landedTick = tick // the next is sent a gap after this reading, as the standing loop does
		if e.misses >= 3 {
			e.done, e.reason = true, "stalled"
		}
		return false
	}
	return true
}

// enchantPoll is one walk poll of the enchant: settle the last cast, then cast if every gate passes. It reports
// whether the enchant keeps the walk's casts (false once it is done or refused, so the walk's alchemy and rub go on).
func (t *Travel) enchantPoll(d *Deps, p webwalk.AsidePoll) bool {
	e := &t.ench
	st := d.Session.State()
	now, tick := t.now(), st.Tick()
	if t.enchantSeen(d, now, tick) {
		return true
	}
	if e.done {
		return false
	}
	// The cheap gates first, before anything reads the pack.
	ticksSince := 1 << 20
	if e.landedTick >= 0 {
		ticksSince = tick - e.landedTick
	}
	if p.SinceClick < fleetplan.WalkAlchAfterClick || ticksSince < e.gap {
		return true
	}
	running := st.RunEnabled()
	ahead := tile.Chebyshev(tile.Tile{X: p.Me.X, Z: p.Me.Z}, tile.Tile{X: p.Target.X, Z: p.Target.Z})
	if ahead < fleetplan.WalkEnchantAhead(running, p.LegEnd) {
		return true
	}
	pack, slot, pieces := t.enchantPieces(d)
	if pieces == 0 {
		e.done, e.reason = true, "done"
		return false
	}
	main, side, _ := st.Modals()
	hp, maxHP := ownHitpoints(d)
	dec := fleetplan.ShouldWalkEnchant(fleetplan.WalkEnchantPoll{
		SinceClick: p.SinceClick, Pending: e.pending, TicksSinceLand: ticksSince, GapTicks: e.gap,
		Ahead: ahead, LegEnd: p.LegEnd, Running: running, InCombat: st.InCombat(),
		Wilderness: fleetplan.WildernessLevel(tile.Tile{X: p.Me.X, Z: p.Me.Z}),
		Modal:      main != gamestate.NoModal || side != gamestate.NoModal, HP: hp, MaxHP: maxHP, NoFood: noFoodAtLowHP(d, hp, maxHP), Pieces: pieces,
	})
	if !dec.Cast {
		return true
	}
	if err := t.sendSpell(d, e.cfg.Spell, pack, slot); err != nil {
		t.sayOnce(d, "enchant-send", "walk enchant: sending the cast: "+err.Error())
		return true
	}
	e.pending, e.sentAt, e.before = true, now, PackCountOf(d, int(e.cfg.FinalID))
	e.sent++
	return true
}

// EnchantWalkTake ends the walk's enchanting and says what it did. A cast still on its way is waited for (at most
// four ticks), and when pieces are left and the last cast landed a moment ago the call waits out the spell's delay,
// so the standing cast that follows is not sent inside it and dropped. Safe on a nil Travel, and when nothing was
// armed.
func (t *Travel) EnchantWalkTake(d *Deps) (res EnchantWalkResult) {
	if t == nil || !t.ench.on {
		return res
	}
	e := &t.ench
	st := d.Session.State()
	const step = 10 * time.Millisecond
	for i := 0; e.pending && i <= int(enchantWalkLandTicks*t.TickLen()/step)+1; i++ {
		if !t.enchantSeen(d, t.now(), st.Tick()) {
			break
		}
		if !d.Delay(step) {
			break
		}
	}
	e.pending = false // a cast still unseen is not counted; the standing step will find the pack as it is
	if _, _, left := t.enchantPieces(d); left > 0 && !e.done && e.landedTick >= 0 {
		for i := 0; st.Tick() < e.landedTick+e.gap && i <= int(time.Duration(e.gap+1)*t.TickLen()/step); i++ {
			if !d.Delay(step) {
				break
			}
		}
	}
	_, _, left := t.enchantPieces(d)
	res = EnchantWalkResult{
		Sent: e.sent, Landed: e.landed, XP: MagicXP(d) - e.xp0, Ticks: st.Tick() - e.tick0, Left: left, Reason: e.reason,
	}
	if res.Reason == "" && left == 0 {
		res.Reason = "done"
	}
	t.ench = enchantWalkState{}
	return res
}
