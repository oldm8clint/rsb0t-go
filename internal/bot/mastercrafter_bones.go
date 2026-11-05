package bot

// The bone-burying round (a user request: swarm accounts past Prayer 43 save every bone for MasterCrafter, who buries them at a bank
// "as burying bones at a bank is the fastest exp per hour and we want master to get 200m prayer first"). The rules, the content facts and
// the stream's arithmetic are mastercraftplan/bones.go's; the run is mastercraftrunner.FastBones: Withdraw-All of the best bone, 28 Bury
// events, the booth op, six ticks a load.
//
//	trigger   the mule's published bank holds craftBonesMinStock bones (5,000), Prayer under 200M xp; a cut-short round resumes from this
//	          account's own bank while it holds a load.
//	where     the mule's post, its bank at Catherby (the bow, pearl and staff rounds' place): the booth is the Use-quickly that ends each load.
//	asks      one line of the best bone, in whole loads up to craftKitSize, as notes; again each time the bank holds under a load.
//	end       no load left in the bank or at the mule, the hold, or the round's hour (craftBonesRoundMinutes); the pause after is
//	          craftBonesPauseMinutes (0: the rotation shares the time).
//
// Nothing here is lab-proven (owner rule): grep "mastercrafter: bones:" in the log.

import (
	"fmt"
	"time"

	"rsb0t-go/internal/randomevents"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/mastercraftplan"
	"rsb0t-go/internal/scripts/mastercraftrunner"
)

// masterBonesAsks bounds the kit asks one round makes: an hour at 28 Big bones every 3.6 s is 28,000 bones, 1,000 an ask.
const masterBonesAsks = 40

// bonesSnapshot fills the round's part of a decision snapshot: Prayer experience and this account's bank's bones.
func (b *masterCrafterBot) bonesSnapshot(ctx *Context, s *mastercraftplan.Snapshot) {
	s.PrayerXP = masterSkillXP(ctx, mastercraftrunner.PrayerStat)
	if bank := ownBankItems(ctx); bank != nil {
		s.OwnBones = mastercraftrunner.BonesOwn(bank)
	}
}

// bonesRound is the bone-burying round: see the file comment.
func (b *masterCrafterBot) bonesRound(ctx *Context, d *fleetrunner.Deps, started time.Time, dec mastercraftplan.Decision) masterRoundOutcome {
	out := masterRoundOutcome{Mode: mastercraftplan.ModeBones, RotationKey: mastercraftplan.KeyBonesRotation}
	post, why, ok := b.muleHere(ctx)
	if !ok {
		out.Transient, out.Why = true, why
		return out
	}
	if why, ok := b.arrive(ctx, d, post); !ok {
		out.Transient, out.Why = true, why
		return out
	}
	xp0 := masterSkillXP(ctx, mastercraftrunner.PrayerStat)
	b.logf(ctx, "bones: trigger: %s; resume=%v; Prayer xp %d", dec.Why, dec.Resume, xp0)
	var qb mastercraftrunner.QuickBank

	v := mastercraftrunner.FullVisit(d, post, &qb, nil, nil, false)
	if !v.Opened || v.Err != "" {
		out.Transient, out.Why = true, "the bank at the mule's post: "+v.Err
		return out
	}
	held := mastercraftrunner.BonesOwn(v.Banked)
	asks, buried, fails, desyncs := 0, 0, 0, 0
	reason := ""
	limited := false
	fastHold := func() bool {
		if mastercraftplan.LimitReached(b.set, mastercraftplan.ModeBones, "", started, time.Now()) {
			limited = true
			return false
		}
		return !ctx.Stopped() && !mastercraftplan.RoundExpired(started, time.Now(), b.set.MaxRound) &&
			!ctx.Session.State().IsDead() && !randomevents.Busy(ctx.Session)
	}
	var guarded time.Time
	hold := func() bool {
		if ctx.Stopped() {
			reason = "stopped"
			return false
		}
		if mastercraftplan.LimitReached(b.set, mastercraftplan.ModeBones, "", started, time.Now()) {
			limited = true
			reason = "the round's hour (craftBonesRoundMinutes) is used up"
			return false
		}
		if mastercraftplan.RoundExpired(started, time.Now(), b.set.MaxRound) {
			reason = "the round's time (craftMaxRoundMinutes) is used up"
			return false
		}
		if time.Since(guarded) < 500*time.Millisecond {
			return true
		}
		guarded = time.Now()
		masterClearDead(ctx)
		if !fleetrunner.WaitForGuardian(d) {
			reason = "stopped"
			return false
		}
		return true
	}
	packBanked := true
	var pace masterPace
	for hold() {
		masterDismissChat(ctx)
		if _, ok := mastercraftplan.BestBone(held); !ok {
			lines := mastercraftplan.PlanBonesAsk(b.set, b.muleStock(ctx))
			if len(lines) == 0 || asks >= masterBonesAsks {
				reason = "no load of bones left in the bank or at the mule"
				break
			}
			asks++
			a := b.askMuleWith(ctx, d, lines, masterAskOpts{noted: true, banked: packBanked})
			b.logf(ctx, "bones: asked %s for %s: %s", b.mule, describeLines(lines), masterDescribe(a.Got))
			if a.Stopped {
				reason = "stopped"
				break
			}
			if a.Traded == 0 {
				out.Transient = !a.declinedEmpty()
				reason = a.Why
				break
			}
			if v = mastercraftrunner.FullVisit(d, post, &qb, nil, nil, false); !v.Opened || v.Err != "" {
				out.Transient, reason = true, "the bank at the mule's post: "+v.Err
				break
			}
			held = mastercraftrunner.BonesOwn(v.Banked)
			packBanked = true
			continue
		}
		if !qb.Ready(d) {
			if v = mastercraftrunner.FullVisit(d, post, &qb, nil, nil, false); !v.Opened || v.Err != "" || !qb.Ready(d) {
				if fails++; fails >= 3 {
					out.Transient, reason = true, "no quick bank at the mule's post: "+v.Err
					break
				}
				ctx.Delay(3 * time.Second)
				continue
			}
			packBanked = true
		}
		if !qb.Primed() && !qb.Prime(d) {
			qb.Forget()
			if fails++; fails >= 3 {
				out.Transient, reason = true, "the quick bank would not prime"
				break
			}
			continue
		}
		fr := mastercraftrunner.FastBones(d, &qb, mastercraftrunner.FastOptions{MaxLoads: masterFastMaxLoads, Hold: fastHold})
		packBanked = false
		if fr.Cuts > 0 {
			buried += fr.Cuts
			pace.addFast(fr.Loads, fr.Took, fr.Ticks, fr.XP)
			// The bank holds that many fewer of the best kind; the next full visit reads the real count.
			if k, ok := mastercraftplan.BestBone(held); ok {
				held[k.Name] = max(held[k.Name]-fr.Cuts, 0)
			}
		}
		if fr.Reason != "not-fast" {
			b.logf(ctx, "bones: %s; %s", fr.Describe(), pace.say())
		}
		switch fr.Reason {
		case "done":
			fails, desyncs = 0, 0
		case "not-fast":
			packBanked = true
			if fr.Short {
				held = map[string]int{}
			}
		case "desync":
			pace.desyncs++
			if desyncs++; desyncs >= masterFastMaxDesyncs {
				reason = fmt.Sprintf("%d six-tick runs in a row fell out of step", desyncs)
				out.Transient = true
			}
			// The pack may hold unburied bones: the next full visit banks them and reads the bank again.
			if v = mastercraftrunner.FullVisit(d, post, &qb, nil, nil, false); v.Opened && v.Err == "" {
				held = mastercraftrunner.BonesOwn(v.Banked)
			}
		case "no-open":
			qb.Forget()
			if fails++; fails >= 3 {
				out.Transient, reason = true, "the bank did not open for the fast run: "+fr.Why
			}
		}
		if reason != "" {
			break
		}
	}
	gained := masterSkillXP(ctx, mastercraftrunner.PrayerStat) - xp0
	b.ledger.Bones.Rounds++
	b.ledger.Bones.Buried += buried
	b.ledger.Bones.XP10 += gained * 10
	out.Produced = buried
	if limited {
		out.LimitReached = true
		b.logf(ctx, "bones: %s", mastercraftplan.LimitWhy(mastercraftplan.ModeBones, b.set.Sched.BonesRound, b.set.Sched.BonesPause))
	}
	out.Why = fmt.Sprintf("%d bones buried for %d Prayer xp in %d ask(s) (%s)", buried, gained, asks, firstNonEmpty(reason, "stopped"))
	b.logf(ctx, "bones: round over: %s; %s", out.Why, pace.say())
	return out
}
