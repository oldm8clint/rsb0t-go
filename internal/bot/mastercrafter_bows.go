package bot

// MasterCrafter's bow round strings four ticks a load of 14 (a user request: "make sure we speed
// up bow stringing"): mastercraftrunner.FastBows, offered to the embedded BowStringer before each of
// its passes (bowStringerEmbed.fast). It runs while the bank at the mule's post holds a load of
// bows and strings; when it does not, it steps aside and BowStringer's own bank step strings the
// remainder and orders the next kit from the mule, and the run takes over again once the kit has
// landed (notes in the pack).

import (
	"time"

	"rsb0t-go/internal/randomevents"
	"rsb0t-go/internal/scripts/bowstringplan"
	"rsb0t-go/internal/scripts/bowstringrunner"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/mastercraftplan"
	"rsb0t-go/internal/scripts/mastercraftrunner"
	"rsb0t-go/internal/scripts/tile"
)

// masterFastBows is one bow round's fast runs.
type masterFastBows struct {
	qb mastercraftrunner.QuickBank
	// off ends the runs for the round; idle waits, after the bank could not fund a load of
	// idleBow, until a kit has landed (the embedded BowStringer's landed count moves past
	// idleLanded, or a kit's notes are still in the pack) or BowStringer moves on to another bow.
	off, idle  bool
	idleBow    string
	idleLanded int
	desyncs    int
	pace       masterPace
}

// waiting reports whether the runs for bow are still waiting for a kit, and stops waiting once
// one has landed (BowStringer's landed count has moved past idleLanded, or a kit's notes are in
// the pack) or BowStringer has moved on to another bow.
//
// THE BOW IS PART OF IT. Live on def92a2e (5 Oct 2026) the Yew longbows ran out, the wait began,
// and BowStringer moved on to the Yew shortbows the main account's own bank held 30,000 of: no kit was ever
// going to land, so every shortbow went on BowStringer's slow trips (~350 in five minutes,
// against 14 a 2.4-second load fast).
func (fb *masterFastBows) waiting(bow string, landed int, packOther bool) bool {
	if !fb.idle {
		return false
	}
	if bow == fb.idleBow && landed == fb.idleLanded && !packOther {
		return true
	}
	fb.idle = false
	return false
}

// ownBows is the bow round's items in this account's bank as the session last saw it: the
// unstrung bows of every bow it strings, by name, and the Bow strings (mastercraftplan.OwnBows).
func (b *masterCrafterBot) ownBows(ctx *Context) mastercraftplan.OwnBows {
	bank := ownBankItems(ctx)
	if bank == nil || ctx.Config == nil {
		return mastercraftplan.OwnBows{}
	}
	byID := map[int32]int{}
	for _, it := range bank {
		byID[it.ID] += int(it.Count)
	}
	own := mastercraftplan.OwnBows{Unstrung: map[string]int{}}
	for _, name := range b.set.BowNames {
		ids, err := bowstringrunner.ResolveIDs(ctx.Config, name)
		if err != nil {
			continue
		}
		if n := byID[int32(ids.Unstrung)]; n > 0 {
			own.Unstrung[name] = n
		}
		own.Strings = byID[int32(ids.String)]
	}
	return own
}

// packHasOther reports whether the pack holds anything but the bow's own three items: a kit that
// landed as notes.
func packHasOther(d *fleetrunner.Deps, ids bowstringrunner.IDs) bool {
	inv := d.Session.State().Inventory(fleetrunner.PlayerPack)
	if inv == nil {
		return false
	}
	for _, it := range inv.NonEmpty() {
		if it.ID != ids.Unstrung && it.ID != ids.Strung && it.ID != ids.String {
			return true
		}
	}
	return false
}

// fastBowHook is the bow round's bowStringerEmbed.fast: see the file comment. bs is the embedded
// BowStringer, whose strung counts the runs add to.
func (b *masterCrafterBot) fastBowHook(post tile.Tile, started time.Time, bs *bowStringerBot) func(*Context, *fleetrunner.Deps, bowstringplan.Bow, bowstringrunner.IDs) bool {
	fb := &masterFastBows{off: !b.set.FastBows}
	return func(ctx *Context, d *fleetrunner.Deps, bow bowstringplan.Bow, ids bowstringrunner.IDs) bool {
		masterDismissChat(ctx) // a level-up left open stops the uses and the bank alike (mastercrafter_chat.go)
		if fb.off {
			return false
		}
		xp, ok := bowstringplan.XPTenths(bow.Name)
		if !ok {
			return false
		}
		// Waiting for the kit BowStringer orders. It banks the kit's notes itself (order), so the
		// pack is empty again by the next pass; waiting on the pack alone kept the round on
		// BowStringer's slow trips for good after its first kit (live 0050e8c9, 5 Oct 2026: 280
		// bows in 50 s fast, then 350 in five minutes).
		if fb.waiting(bow.Name, bs.landed, packHasOther(d, ids)) {
			return false
		}
		if !fb.qb.Ready(d) {
			v := mastercraftrunner.FullVisit(d, post, &fb.qb, nil, nil, false)
			if !v.Opened || v.Err != "" || !fb.qb.Ready(d) {
				b.logf(ctx, "bows: no quick bank at the mule's post (%s); stringing the usual way for the rest of the round", v.Err)
				fb.off = true
				return false
			}
		}
		if !fb.qb.Primed() && !fb.qb.Prime(d) {
			fb.qb.Forget()
			return false
		}
		hold := func() bool {
			return !ctx.Stopped() && !mastercraftplan.RoundExpired(started, time.Now(), b.set.MaxRound) &&
				!ctx.Session.State().IsDead() && !randomevents.Busy(ctx.Session)
		}
		fr := mastercraftrunner.FastBows(d, &fb.qb, bow.Name, ids, xp, mastercraftrunner.FastOptions{MaxLoads: masterFastMaxLoads, Hold: hold})
		if fr.Cuts > 0 {
			bs.strung[bow.Name] += fr.Cuts
			lootStrung(ctx, ids.Unstrung, ids.Strung, bow.Name, fr.Cuts)
			fb.pace.addFast(fr.Loads, fr.Took, fr.Ticks, fr.XP)
		}
		if fr.Reason != "not-fast" {
			b.logf(ctx, "bows: %s: %s; %s", bow.Name, fr.Describe(), fb.pace.say())
		}
		switch fr.Reason {
		case "done":
			fb.desyncs = 0
			return true
		case "not-fast":
			if fr.Short {
				fb.idle, fb.idleBow, fb.idleLanded = true, bow.Name, bs.landed // the bank cannot fund a load: BowStringer strings the rest and orders a kit
				return false
			}
			return true // the pack was banked whole: the next pass runs from the empty pack
		case "desync":
			fb.pace.desyncs++
			if fb.desyncs++; fb.desyncs >= masterFastMaxDesyncs {
				fb.off = true
				b.logf(ctx, "bows: %d four-tick runs in a row fell out of step; stringing the usual way for the rest of the round", fb.desyncs)
			}
			return true // the next pass banks whatever the pack holds and starts again
		case "refused", "members":
			fb.off = true
			b.logf(ctx, "bows: stringing %s was refused (%s); stringing the usual way for the rest of the round", bow.Name, fr.Reason)
			return false
		case "no-open":
			fb.qb.Forget()
			return false
		default: // held, stopped: BowStringer's own loop sees to the guardian and the stop
			return true
		}
	}
}
