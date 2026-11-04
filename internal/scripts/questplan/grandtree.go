package questplan

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// The Grand Tree (content id `grandtree`, %grandtree, not transmitted), from the lane Q study
// (private evidence notes) and the TS def
// (rsb0t-main/src/bot/api/ai/quests/defs/grandtree), whose journal needles and tiles the study
// checked against content. Content enforces Agility 25 at one place, the tree up to Glough's
// pillar floor (stages 120 and 130). The Black Demon at the end (172, max 16, 157 HP, magic
// defence -10) is the study's open risk: no safespot was confirmed, and the TS def proved it at
// 70 across the board with Protect from Melee. This port used to cast at it when a spell was
// castable and melee otherwise, with the food the kit brought; it did not claim the fight was
// survivable at the ladder's combat level, and live on 5 Oct 2026 it was not (see GTDemonReady:
// emod died four times and acct9 once, each after eating 14-18 sharks in two to four minutes).
// The demon is now skipped, with a day's park and a plain log line, unless the account can fight
// it the one way that works.

// GrandTreeName is the quest tab's spelling (component 7361).
const GrandTreeName = "The Grand Tree"

// GrandTreeID is the engine's id (the ladder's rung id).
const GrandTreeID = "grandtree"

// Stages (quest_grandtree.constant).
const (
	GTStarted    = 10
	GTHazelmere  = 20
	GTRelayed    = 30
	GTGlough     = 40
	GTPrisoner   = 50
	GTSpokenChar = 60
	GTJournal    = 70
	GTReleased   = 80
	GTLumber     = 90
	GTClue       = 100
	GTPlans      = 110
	GTTwigs      = 120
	GTTrapdoor   = 130
	GTDemon      = 140
	GTDaconia    = 150
	GTComplete   = 160
)

// Ids (quest_grandtree.obj/.loc/.npc).
const (
	ObjGTBark     = 783
	ObjGTJournal  = 785
	ObjGTScroll   = 786
	ObjGTLumber   = 787
	ObjGTKey      = 788
	ObjGTDaconia  = 793
	ObjGTPlans    = 794
	LocGTCupShut  = 2434
	LocGTCupOpen  = 2435
	LocGTChest    = 2436
	LocGTTrapdoor = 2444
	LocGTClimb    = 2447
	LocGTDown     = 2448
	NpcGTDemon    = 677
	NpcNarnode    = 670
	NpcHazelmere  = 669
	NpcGlough     = 671
	NpcAnita      = 672
	NpcCharlie    = 673
	NpcForeman    = 674
	NpcFemi       = 676
	NpcGTPilot    = 170
)

// Twigs and their pillars, in the order the trapdoor's obj_find checks them (TUZO).
var GTPillars = []struct {
	Twig, Pillar int
	Stand        tile.Tile
}{
	{789, 2440, tile.Tile{X: 2485, Z: 3466, Level: 2}},
	{790, 2441, tile.Tile{X: 2486, Z: 3466, Level: 2}},
	{791, 2442, tile.Tile{X: 2487, Z: 3466, Level: 2}},
	{792, 2443, tile.Tile{X: 2488, Z: 3466, Level: 2}},
}

// GTRoots are the fifteen daconia_coords roots (the rock's root is rolled server-side).
var GTRoots = []struct{ SW, Stand tile.Tile }{
	{tile.Tile{X: 2456, Z: 9886}, tile.Tile{X: 2455, Z: 9885}}, {tile.Tile{X: 2457, Z: 9881}, tile.Tile{X: 2456, Z: 9882}},
	{tile.Tile{X: 2455, Z: 9874}, tile.Tile{X: 2454, Z: 9875}}, {tile.Tile{X: 2443, Z: 9878}, tile.Tile{X: 2442, Z: 9877}},
	{tile.Tile{X: 2439, Z: 9881}, tile.Tile{X: 2438, Z: 9881}}, {tile.Tile{X: 2444, Z: 9893}, tile.Tile{X: 2443, Z: 9892}},
	{tile.Tile{X: 2452, Z: 9893}, tile.Tile{X: 2451, Z: 9892}}, {tile.Tile{X: 2465, Z: 9891}, tile.Tile{X: 2464, Z: 9890}},
	{tile.Tile{X: 2468, Z: 9890}, tile.Tile{X: 2467, Z: 9889}}, {tile.Tile{X: 2467, Z: 9896}, tile.Tile{X: 2466, Z: 9895}},
	{tile.Tile{X: 2473, Z: 9897}, tile.Tile{X: 2472, Z: 9896}}, {tile.Tile{X: 2481, Z: 9904}, tile.Tile{X: 2480, Z: 9903}},
	{tile.Tile{X: 2485, Z: 9885}, tile.Tile{X: 2484, Z: 9884}}, {tile.Tile{X: 2490, Z: 9889}, tile.Tile{X: 2489, Z: 9888}},
	{tile.Tile{X: 2467, Z: 9872}, tile.Tile{X: 2466, Z: 9871}},
}

// Tiles (TS areas.ts).
var (
	NarnodeAnchor   = tile.Tile{X: 2466, Z: 3497}
	NarnodeUnder    = tile.Tile{X: 2464, Z: 9896}
	HazelmereAnchor = tile.Tile{X: 2678, Z: 3086, Level: 1}
	GloughAnchor    = tile.Tile{X: 2478, Z: 3463, Level: 1}
	GloughFoot      = tile.Tile{X: 2476, Z: 3464}
	GTCupStand      = tile.Tile{X: 2476, Z: 3464, Level: 1}
	GTCupTile       = tile.Tile{X: 2476, Z: 3465, Level: 1}
	GTChestStand    = tile.Tile{X: 2482, Z: 3463, Level: 1}
	GTClimbStand    = tile.Tile{X: 2484, Z: 3463, Level: 1}
	GTDownStand     = tile.Tile{X: 2485, Z: 3465, Level: 2}
	GTPillarFloor   = tile.Tile{X: 2486, Z: 3465, Level: 2}
	GTTrapdoorStand = tile.Tile{X: 2487, Z: 3465, Level: 2}
	GTCaveLadder    = tile.Tile{X: 2463, Z: 9896}
	CharlieAnchor   = tile.Tile{X: 2465, Z: 3495, Level: 3}
	GTJail          = tile.Tile{X: 2464, Z: 3496, Level: 3}
	GTGliderPad     = tile.Tile{X: 2465, Z: 3501, Level: 3}
	GTKaramjaCrash  = tile.Tile{X: 2917, Z: 3058}
	ForemanAnchor   = tile.Tile{X: 2999, Z: 3043}
	FemiAnchor      = tile.Tile{X: 2460, Z: 3382}
	AnitaStairs     = tile.Tile{X: 2388, Z: 3512}
	AnitaFloor      = tile.Tile{X: 2388, Z: 3513, Level: 1}
	AnitaAnchor     = tile.Tile{X: 2390, Z: 3514, Level: 1}
	// StrongholdBank is the Grand Tree's own bank floor, in front of the booths at 2448-2450,3481.
	// It was the south-west bank at 2445,3425,1, which the walker graph has no stair up to: live
	// on 5 Oct 2026 every Black Demon kit failed there ("could not use the bank ... unreachable").
	StrongholdBank = tile.Tile{X: 2449, Z: 3482, Level: 1}
	gtFoods        = []int{385, 373, 379, 365, 361, 329, 333, 2309}
	gtTools        = []int{ObjGTBark, 784, ObjGTJournal, ObjGTScroll, ObjGTLumber, ObjGTKey, 789, 790, 791, 792, ObjGTDaconia,
		ObjGTPlans, CoinsID, 554, 556, 558, 562, 1381}
	GTHops = []LadderHop{{Stand: GTCaveLadder, LocName: "Ladder", Op: "Climb-up", Arrive: tile.Tile{X: 2463, Z: 3497}}}
)

// THE BLACK DEMON, from content (quest_grandtree/configs/quest_grandtree.npc [grandtree_blackdemon],
// npc 677, and scripts/grandtree_black_demon.rs2, glough.rs2):
//
//	size=3 vislevel=172 attack=145 strength=148 defence=152 hitpoints=157 param=magicdefence,-10
//	wanderrange=20 maxrange=20 timer=20; no huntmode, no ai_applayer2: plain melee (npc_default_attack)
//
// Glough's cutscene spawns it at 2479,9867 (npc_add, 1000 ticks) and ~npc_retaliate(0) points it at
// the account at once; its ai_timer deletes it when the account is gone or more than 17 tiles away.
// Its maximum hit is ((148+9)*64+320)/640 = 16, its attack roll (145+9)*64, so against an account
// with Defence 40 and no armour 84% of its swings land for 8 on average: about 2.8 hitpoints a
// second, with 16 possible from the first swing. The account cannot out-eat it (acct9, 44
// hitpoints, ate 14 sharks and 4 lobsters in 3.5 minutes and died), melee against Defence 152 is
// hopeless (an Attack 55, Strength 55 account would need eleven minutes and take about 1,800
// hitpoints: see MeleeExchange), and a Wind Bolt caster needs about 114 seconds of casts in which
// the demon deals about 320.
//
// NO SAFESPOT, checked on the real collision pack (grandtree_pack_test.go). The demon's 3x3 body
// cannot leave a chamber around its spawn (74 body positions, footprint x 2474-2491, z 9860-9871): every
// passage out is too narrow. The account arrives on that chamber's east edge (2491,9864). Of the
// 188 tiles within 30 steps of it, 51 are out of the body's reach, but the nearest from which a
// spell sees five or more of the 74 positions (2472,9866) is 19 steps away and 18 of them are on
// tiles the body can melee: nine ticks at a run, in which the demon, five tiles from the arrival
// tile when Glough's cutscene lets the account go, swings several times for up to 16. The two
// nearer safe tiles (6 and 7 steps) see none or one of the 74. The model is more generous to the
// demon than its naive pathing, so a tile it calls safe is safe; nothing here is a tile an
// account can reach before the demon's first swing.
//
// WHAT WORKS is Protect from Melee (prayers.dbrow level 43): check_protect_prayer zeroes every
// melee hit (playerhit_n_melee), the way the TS def beat it. It drains one point every three
// seconds (drain effect 12), so Prayer 43 holds it for 129 seconds, and the kill has to fit: a
// Wind Blast caster (Magic 41, death runes) needs about 80. Live on 5 Oct 2026 no account has
// Prayer 43 (0 of 313 ScarabSwarm accounts; 4 have 37), and the step has no code that holds the
// prayer, so GTDemonFightBuilt is false and the quest waits.
const (
	GTDemonHitpoints = 157
	GTDemonMaxHit    = 16
	// GTDemonPrayerLevel is Protect from Melee's level (content prayers.dbrow).
	GTDemonPrayerLevel = 43
	// GTDemonParkFor is how long an account that cannot fight the demon skips the quest: a day, as
	// MaxDeaths does for deaths, because nothing about the account changes inside an hour.
	GTDemonParkFor = 24 * time.Hour
)

// GTDemonFightBuilt is whether questmods holds Protect from Melee through the Black Demon fight (on
// at the demon's cutscene, held while casting, off after, with a bail on prayer points). It is not
// built: no account could use it, and an unproven boss fight is not shipped for none. Everything
// that makes the demon beatable is named in GTDemonReady, and the step refuses to descend to it
// while this is false.
const GTDemonFightBuilt = false

// GTDemonVerdict is GTDemonReady's answer.
type GTDemonVerdict struct {
	OK bool
	// Why is the reason the quest skips, in one line fit for a log: what the demon is, what it
	// takes, and what this account has.
	Why string
}

// GTDemonReady judges whether the account may take the Black Demon on. It may when it has Protect
// from Melee (Prayer 43) and the fight that holds it exists; otherwise the quest skips for
// GTDemonParkFor with a line that says so. An account whose Prayer level is not read yet is judged
// as Prayer 0.
func GTDemonReady(s Snapshot) GTDemonVerdict {
	prayer := s.Level("prayer")
	switch {
	case prayer < GTDemonPrayerLevel:
		return GTDemonVerdict{Why: fmt.Sprintf("not doing The Grand Tree: it ends at the Black Demon (level 172, %d hitpoints, hits up to %d every 4 ticks), which cannot be tanked, out-eaten or hidden from in its cave and is only beaten with Protect from Melee held through the fight (Prayer %d; this account has %d)",
			GTDemonHitpoints, GTDemonMaxHit, GTDemonPrayerLevel, prayer)}
	case !GTDemonFightBuilt:
		return GTDemonVerdict{Why: fmt.Sprintf("not doing The Grand Tree: the Black Demon needs Protect from Melee held through the fight, which the step does not do yet (this account's Prayer %d is enough)", prayer)}
	}
	return GTDemonVerdict{OK: true}
}

// InGTCaves is the root caves under the Grand Tree.
func InGTCaves(t tile.Tile) bool { return t.Z >= 9856 && t.Z <= 9919 && t.X >= 2420 && t.X <= 2500 }

// InKaramjaJungle is the far side of the glider ride.
func InKaramjaJungle(t tile.Tile) bool { return t.X >= 2850 && t.Z <= 3200 && t.Z > 2900 }

// InStronghold is inside the Gnome Stronghold's walls.
func InStronghold(t tile.Tile) bool {
	return t.X >= 2370 && t.X <= 2540 && t.Z >= 3384 && t.Z <= 3540
}

var gtJournal = []struct {
	stage  int
	needle string
}{
	{GTComplete, "quest complete!"},
	{GTDaconia, "king narnode has seen the light"},
	{GTDemon, "he should be in these caves somewhere"},
	{GTTrapdoor, "i should investigate what's behind the"},
	{GTTwigs, "isn't convinced but gave me some"},
	{GTPlans, "i should talk to the king and see what he says"},
	{GTClue, "has suggested i ask"},
	{GTLumber, "i have an invoice that the foreman had"},
	{GTReleased, "has told me to use the"},
	{GTJournal, "i should go speak to him again"},
	{GTSpokenChar, "and agreed to search glough's house"},
	{GTPrisoner, "said i can speak to"},
	{GTGlough, "maybe i should have a chat with"},
	{GTRelayed, "the king wants me to tell"},
	{GTHazelmere, "i then need to give the message"},
	{GTStarted, "he has asked me to take a"},
	{0, "i can start this quest at the"},
}

// ParseGrandTreeJournal reads grandtree_journal.rs2, newest first (TS journal.ts's needles).
func ParseGrandTreeJournal(lines []string) (int, bool) {
	text := NormaliseLines(lines)
	if text == "" {
		return 0, false
	}
	for _, n := range gtJournal {
		if strings.Contains(text, n.needle) {
			return n.stage, true
		}
	}
	return 0, false
}

// Custom step names (internal/scripts/questmods).
const (
	CustomGTStart    = "gt-start"
	CustomGTCupboard = "gt-cupboard"
	CustomGTJailed   = "gt-jailed"
	CustomGTFly      = "gt-fly"
	CustomGTForeman  = "gt-foreman"
	CustomGTFemi     = "gt-femi"
	CustomGTAnita    = "gt-anita"
	CustomGTChest    = "gt-chest"
	CustomGTTwig     = "gt-twig"
	CustomGTDescend  = "gt-descend"
	CustomGTDemon    = "gt-demon"
	CustomGTCaves    = "gt-caves"
	CustomGTRoots    = "gt-roots"
	CustomGTRock     = "gt-rock"
	KitGTDemon       = "gt-demon-kit"
)

var (
	NarnodeStop = NpcStop{Npc: "King Narnode Shareen", ID: NpcNarnode, Anchor: NarnodeAnchor, Leash: 6, Prefer: []string{
		"You seem worried, what's up?", "I'd be happy to help!", "I think so!", "A man came to me with the King's seal.",
		"I gave the man Daconia rocks.", "And Daconia rocks will kill the tree!", "None of the above."}}
	HazelmereStop = NpcStop{Npc: "Hazelmere", ID: NpcHazelmere, Anchor: HazelmereAnchor, Leash: 8, Prefer: []string{"Ok."}}
	GloughStop    = NpcStop{Npc: "Glough", ID: NpcGlough, Anchor: GloughAnchor, Leash: 8, Prefer: []string{"Ok."}}
	CharlieStop   = NpcStop{Npc: "Charlie", ID: NpcCharlie, Anchor: CharlieAnchor, Leash: 4, Prefer: []string{"Ok."}}
)

func gtDemonKit() []KitLine {
	return []KitLine{
		{ID: 385, Name: "Shark", Qty: 20, Optional: true},
		{ID: 379, Name: "Lobster", Qty: 10, Optional: true},
		{ID: 562, Name: "Chaos rune", Qty: 60, Stackable: true, Optional: true},
		{ID: 1381, Name: "Staff of air", Qty: 1, Optional: true, Worn: true},
	}
}

// gtDemonArmed reports whether the pack holds some of the demon kit's food and its chaos runes.
func gtDemonArmed(s Snapshot) bool {
	food := false
	for _, l := range gtDemonKit() {
		if l.Stackable || l.Worn {
			continue
		}
		if s.HeldID(l.ID) > 0 {
			food = true
		}
	}
	return food && s.HeldID(562) > 0
}

// DecideGrandTree is the whole quest (TS index.ts decide, the kit before the trapdoor), behind the
// Black Demon gate (GTDemonReady).
func DecideGrandTree(s Snapshot) Step { return decideGrandTree(s, GTDemonReady) }

// decideGrandTree is DecideGrandTree with the demon gate passed in, so a test can open it and
// exercise the stages the gate hides.
func decideGrandTree(s Snapshot, gate func(Snapshot) GTDemonVerdict) Step {
	switch s.Journal {
	case StatusComplete:
		return Step{Kind: StepDone}
	case StatusUnknown:
		return Step{Kind: StepWait, Reason: "the quest tab has not been coloured yet"}
	}
	st := 0
	if s.Journal == StatusInProgress {
		if !s.StageKnown {
			return Step{Kind: StepWait, Reason: "the journal stage has not been read"}
		}
		st = s.Stage
	}
	// The whole quest waits for the account that cannot take the demon at its end: the walk to it is
	// an hour or more, and every account that made it died (emod four times, acct9 once, 5 Oct).
	if st < GTDemon {
		if v := gate(s); !v.OK {
			return Step{Kind: StepWait, Reason: v.Why, ParkFor: GTDemonParkFor}
		}
	}
	held := func(id int) bool { return s.HeldID(id) > 0 }
	keep := append(append([]int(nil), gtTools...), gtFoods...)
	here := s.TileKnown
	inCaves := here && InGTCaves(s.Tile)
	room := func(n int) (Step, bool) {
		if s.FreeSlots >= n {
			return Step{}, false
		}
		return Step{Kind: StepDeposit, KeepIDs: keep, Bank: StrongholdBank, BankSet: true}, true
	}
	if (st == GTTwigs || st == GTTrapdoor) && !inCaves && !s.Ran["provision:"+KitGTDemon] && !s.Failed["provision:"+KitGTDemon] {
		if here && s.Tile.Level > 0 && s.Tile.X > 2470 && s.Tile.Z < 3470 {
			return Step{Kind: StepCustom, Custom: CustomGTDescend, Reason: "down out of Glough's tree for the kit"}
		}
		return Step{Kind: StepProvision, KitName: KitGTDemon, Kit: gtDemonKit(), KeepIDs: keep, Bank: StrongholdBank, BankSet: true,
			Reason: "food and runes for the Black Demon"}
	}
	// The Black Demon is not fought without the kit. Its lines are all Optional so a partial kit
	// still goes, but live on 5 Oct 2026 mackerel's provision failed outright ("could not use the
	// bank at 2445,3425,1", four times) and it went down the trapdoor anyway with an empty pack and
	// died to the demon three times. With the kit tried and failed, and neither food nor the
	// runes in the pack, the quest waits (and so parks) above ground.
	// The kit's lines are all Optional, so a round that found nothing still reads SATISFIED: live on
	// 5 Oct 2026 acct9's kit drew 5 sharks and no runes ("satisfied") and the check above, which
	// only looked after a failed kit, let it go down to the demon, where it died. The pack is judged
	// whenever the kit has been tried, however it ended.
	if st == GTTrapdoor && !inCaves && (s.Failed["provision:"+KitGTDemon] || s.Ran["provision:"+KitGTDemon]) && !gtDemonArmed(s) {
		return Step{Kind: StepWait, Reason: "the Black Demon kit left the pack without food or chaos runes for it"}
	}
	if st >= GTDemon && st < GTComplete && !inCaves {
		return Step{Kind: StepCustom, Custom: CustomGTDemon, Reason: "back down the trapdoor into the root caves"}
	}
	switch st {
	case 0:
		if step, ok := room(2); ok {
			return step
		}
		return Step{Kind: StepCustom, Custom: CustomGTStart, Reason: "King Narnode starts the quest"}
	case GTStarted:
		if held(ObjGTBark) {
			return Step{Kind: StepTalk, Stop: HazelmereStop}
		}
		if step, ok := room(2); ok {
			return step
		}
		return Step{Kind: StepTalk, Stop: NarnodeStop}
	case GTHazelmere, GTGlough:
		return Step{Kind: StepTalk, Stop: NarnodeStop}
	case GTRelayed:
		return Step{Kind: StepTalk, Stop: GloughStop}
	case GTPrisoner:
		return Step{Kind: StepTalk, Stop: CharlieStop}
	case GTSpokenChar:
		return Step{Kind: StepCustom, Custom: CustomGTCupboard, Reason: "Glough's cupboard for his journal"}
	case GTJournal:
		return Step{Kind: StepCustom, Custom: CustomGTJailed, Reason: "Glough, the jail and the King's release"}
	case GTReleased:
		if here && InKaramjaJungle(s.Tile) {
			return Step{Kind: StepCustom, Custom: CustomGTForeman, Reason: "the foreman's lumber order"}
		}
		return Step{Kind: StepCustom, Custom: CustomGTFly, Reason: "the glider to Karamja"}
	case GTLumber:
		if here && InStronghold(s.Tile) {
			return Step{Kind: StepTalk, Stop: CharlieStop}
		}
		return Step{Kind: StepCustom, Custom: CustomGTFemi, Reason: "Femi's cart back into the stronghold"}
	case GTClue, GTPlans:
		if st == GTPlans && held(ObjGTPlans) {
			if step, ok := room(4); ok {
				return step
			}
			return Step{Kind: StepTalk, Stop: NarnodeStop}
		}
		if held(ObjGTKey) {
			return Step{Kind: StepCustom, Custom: CustomGTChest, Reason: "Glough's chest"}
		}
		return Step{Kind: StepCustom, Custom: CustomGTAnita, Reason: "Anita's key to Glough's chest"}
	case GTTwigs:
		for _, p := range GTPillars {
			if held(p.Twig) {
				return Step{Kind: StepCustom, Custom: CustomGTTwig, Reason: "the twigs on their pillars"}
			}
		}
		return Step{Kind: StepTalk, Stop: NarnodeStop}
	case GTTrapdoor:
		return Step{Kind: StepCustom, Custom: CustomGTDemon, Reason: "the trapdoor and the Black Demon"}
	case GTDemon:
		return Step{Kind: StepCustom, Custom: CustomGTRock, Reason: "the King in the caves"}
	case GTDaconia:
		if held(ObjGTDaconia) {
			return Step{Kind: StepCustom, Custom: CustomGTRock, Reason: "the Daconia rock to the King"}
		}
		return Step{Kind: StepCustom, Custom: CustomGTRoots, Reason: "search the roots for the Daconia rock"}
	}
	return Step{Kind: StepWait, Reason: "grand tree: unrecognised stage"}
}

// GrandTreeRecord is the quest's identity: Agility 25 (the tree up to the pillars).
var GrandTreeRecord = Record{ID: GrandTreeID, Name: GrandTreeName, QuestPoints: 5,
	Requirements: Requirements{Skills: []SkillReq{{Skill: "agility", Level: 25}}}}

// GrandTree is the engine module.
var GrandTree = Module{
	Record: GrandTreeRecord, Bank: StrongholdBank, BankSet: true, Anchor: NarnodeAnchor,
	ToolIDs:   gtTools,
	ReadStage: ParseGrandTreeJournal,
	Sustain:   Sustain{FoodIDs: gtFoods, EatAt: 22},
	MaxDeaths: 2,
	// One death at the demon skips the quest for a week: MaxDeaths 2 and a day let emod reach four.
	StepDeathCaps: map[string]StepDeathCap{
		CustomGTDemon: {Max: 1, Window: 7 * 24 * time.Hour, Skip: 7 * 24 * time.Hour},
	},
	Hops:   GTHops,
	Decide: DecideGrandTree,
}
