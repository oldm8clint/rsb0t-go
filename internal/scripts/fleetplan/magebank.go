package fleetplan

import (
	"sort"
	"strings"

	"rsb0t-go/internal/scripts/tile"
)

// Route points. Each is the content coordinate named in this file's doc comment.
var (
	ArdougneLever     = tile.Tile{X: 2561, Z: 3311}
	ArdougneLeverLand = tile.Tile{X: 2562, Z: 3311}
	KeepLever         = tile.Tile{X: 3153, Z: 3923}
	KeepLand          = tile.Tile{X: 3154, Z: 3924}
	// LadderHouseDoor is the open east gap of the ruined house the ladder is in, outside both webs.
	LadderHouseDoor = tile.Tile{X: 3097, Z: 3957}
	// MageBankWebs are the two webs between the gap and the ladder, outermost first.
	MageBankWebs   = []tile.Tile{{X: 3095, Z: 3957}, {X: 3093, Z: 3957}}
	MageBankLadder = tile.Tile{X: 3091, Z: 3957}
	// MageBankLanding is where the ladder puts you (movecoord(coord, -549, 0, 756) from the
	// ladder's own tile area), and MageBankLadderUp the ladder back.
	MageBankLanding  = tile.Tile{X: 2542, Z: 4712}
	MageBankLadderUp = tile.Tile{X: 2542, Z: 4713}
	// MageBankUpApproach is the tile beside the up-ladder that the down-ladder was measured to
	// land on (2543,4713, 2026-09-19): the one tile known to reach it.
	MageBankUpApproach = tile.Tile{X: 2543, Z: 4713}
	GundaiTile         = tile.Tile{X: 2534, Z: 4714}
	LundailTile        = tile.Tile{X: 2534, Z: 4719}
	// KeepWeb is the slashable web (maps/m49_61.jm2) on the way north out of the Deserted Keep
	// that every planned route between the Keep and the ruined house crosses; KeepWebSouth and
	// KeepWebNorth are the tiles either side of it. Crossed by hand (see scarabrunner) rather than
	// by the walker's own web hop: measured live (2026-09-19) the walker gave up on it after three
	// failed hops and then refused to route through it for the rest of the run.
	KeepWeb      = tile.Tile{X: 3158, Z: 3951}
	KeepWebSouth = tile.Tile{X: 3158, Z: 3950}
	KeepWebNorth = tile.Tile{X: 3158, Z: 3952}
	// WildernessSouthExit is where an account walks out of the Wilderness when the Keep lever is
	// out of its reach: the Edgeville bank, south of the Wilderness. From the Rogues' Castle the
	// webwalk route opens the Gate (loc 1596) at 3225,3904 on the way (cmd/webwalkplan, 6 Oct
	// 2026), where the lever's route slashes the Keep web.
	WildernessSouthExit = tile.Tile{X: 3094, Z: 3491}
)

// Names the runner matches, exactly as the config archive resolves them.
const (
	LeverName          = "Lever"
	LeverOp            = "Pull"
	WebName            = "Web"
	WebOp              = "Slash"
	LadderName         = "Ladder"
	LadderDownOp       = "Climb-down"
	LadderUpOp         = "Climb-up"
	GundaiName         = "Gundai"
	GundaiBankOption   = "Cool, I'd like to access my bank account please."
	ArdougneLeverBrave = "Yes I'm brave."
	// WebCutLine / WebFailLine are web.rs2's own messages, the only evidence a slash landed.
	WebCutLine  = "You slash the web apart."
	WebFailLine = "You fail to cut through it."
	WebNoBlade  = "Only a sharp blade can cut through this sticky web."
)

// WildernessLevel is area_wilderness/scripts/wilderness_levels.rs2 ~wilderness_level over the
// two zones wilderness_zones.dbrow lists — the surface (x 2944-3391, z 3520-6399, every level)
// and the Edgeville-dungeon Wilderness (x 2944-3391, z 9920-12799) — each counted up from its
// own bottom edge in eight-tile bands: (z - bottom)/8 + 1. 0 outside both.
func WildernessLevel(t tile.Tile) int {
	if t.X < 2944 || t.X > 3391 {
		return 0
	}
	switch {
	case t.Z >= 3520 && t.Z <= 6399:
		return (t.Z-3520)/8 + 1
	case t.Z >= 9920 && t.Z <= 12799:
		return (t.Z-9920)/8 + 1
	}
	return 0
}

// InMageBank reports whether a tile is in the Mage Bank's own mapsquare (39_73).
func InMageBank(t tile.Tile) bool {
	return t.X >= 2496 && t.X <= 2559 && t.Z >= 4672 && t.Z <= 4735
}

// NearArdougneLever reports whether the account is back in East Ardougne near the lever — the
// trip's start and finish. A distance test against the place, for the reason InArdougneReach
// gives: a region predicate is the wrong tool for "have I arrived".
func NearArdougneLever(t tile.Tile) bool {
	return t.Level == 0 && tile.Chebyshev(tile.Tile{X: t.X, Z: t.Z}, ArdougneLever) <= 60
}

// WildernessBladeWords are the display-name families the owner's "a sword or knife" allows. The
// owner's rule is narrower than content's (a dagger or a battleaxe also slashes a web), and the
// owner's rule wins: this is the ONE item an account may take into the Wilderness.
var WildernessBladeWords = []string{"sword", "scimitar"}

// KnifeItem is content's plain `knife` (obj 946), the only non-weapon web.rs2 accepts.
const KnifeItem = "Knife"

// IsWildernessBlade reports whether an item name is an allowed blade: a Knife, or a sword.
func IsWildernessBlade(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	if n == strings.ToLower(KnifeItem) {
		return true
	}
	for _, w := range WildernessBladeWords {
		if ContainsBladeWord(n, w) {
			return true
		}
	}
	return false
}

// ContainsBladeWord reports whether the lower-case name holds word as a weapon kind: "Bronze sword",
// "Steel longsword" and "Rune 2h sword" do, "Swordfish", "Raw swordfish" and "Burnt swordfish" do
// not. The word may end a longer one ("long|sword") but must not START one: nothing may follow it
// that is a letter or a digit.
//
// LIVE 30 Sep - 2 Oct: a plain substring test made Swordfish -- one of the mule's own foods -- a
// "blade" everywhere this is asked. PrepareMelee drew it from the bank and tried to wield it
// ("wear Swordfish: actions: no option with that label", 910 times on 4 accounts, "could not wield
// Swordfish" 713 more on 2), and the Wilderness kit check passed it as the web-cutter, so two
// accounts walked to the web again and again and were told "Only a sharp blade can cut through this
// sticky web" 1,429 times in 2.5 days. webwalk's own slash-tool test already knew
// (webwalk.IsSlashWeaponName's containsWord); this was the copy that did not.
func ContainsBladeWord(lower, word string) bool {
	for i := 0; i+len(word) <= len(lower); i++ {
		if lower[i:i+len(word)] != word {
			continue
		}
		if end := i + len(word); end < len(lower) && (lower[end] >= 'a' && lower[end] <= 'z' || lower[end] >= '0' && lower[end] <= '9') {
			continue
		}
		return true
	}
	return false
}

// KitItem is one stack in the pack or one worn item.
type KitItem struct {
	Name  string
	Count int
}

// Kit is what the account is wearing and carrying right before it pulls the lever.
type Kit struct {
	Worn []KitItem
	Pack []KitItem
}

// KitVerdict is CheckWildernessKit's answer. OK is true only when there is nothing to fix.
type KitVerdict struct {
	OK bool
	// Blade is the blade found, and BladeWorn whether it is wielded (the Slash op) or carried (a
	// knife, used on the web).
	Blade     string
	BladeWorn bool
	Coins     int
	// Problems says exactly what is wrong, one line each, so a refusal can be logged honestly.
	Problems []string
}

// CheckWildernessKit enforces the owner's 2026-09-19 Wilderness rule on a kit: ONLY a sword or
// knife — nothing else worn or held: no staff, no food, no other gear, and NO COINS (the purchase
// coins come out of the shared bank at Gundai, inside the Mage Bank). Whatever is carried is what
// a death costs.
//
// A sword may be worn or carried; a knife can only be carried (it is not wieldable). Exactly one
// blade is allowed. A sword in the pack rather than the hand is accepted but flagged by
// BladeWorn=false, because it then depends on use-item-on-web.
func CheckWildernessKit(k Kit) KitVerdict {
	v := KitVerdict{}
	blades := 0
	for _, w := range k.Worn {
		if IsWildernessBlade(w.Name) && !strings.EqualFold(w.Name, KnifeItem) {
			blades++
			v.Blade, v.BladeWorn = w.Name, true
			continue
		}
		v.Problems = append(v.Problems, "wearing "+w.Name+" — nothing but a sword may be worn")
	}
	for _, it := range k.Pack {
		switch {
		case strings.EqualFold(it.Name, "Coins"):
			v.Coins += it.Count
			v.Problems = append(v.Problems, "carrying "+Itoa(it.Count)+" coins — the purchase coins are drawn at Gundai, never carried across")
		case IsWildernessBlade(it.Name):
			blades++
			if v.Blade == "" {
				v.Blade = it.Name
			}
		default:
			v.Problems = append(v.Problems, "carrying "+describeStack(it)+" — only the blade may be carried")
		}
	}
	switch {
	case blades == 0:
		v.Problems = append(v.Problems, "no sword or knife — the webs in front of the Mage Bank ladder cannot be cut")
	case blades > 1:
		v.Problems = append(v.Problems, "more than one blade — take exactly one")
	}
	sort.Strings(v.Problems)
	v.OK = len(v.Problems) == 0
	return v
}

func describeStack(it KitItem) string {
	if it.Count > 1 {
		return Itoa(it.Count) + " x " + it.Name
	}
	return it.Name
}

// PlannedBuy is one rune line of a trip's purchase.
type PlannedBuy struct {
	Rune  string
	Count int
	Cost  int
	// AssumedStock is the stock the cost was priced from (an estimate; see StockLine.EstimateStock).
	AssumedStock int
}

// PurchasePlan is a whole trip's shopping list, and Coins is exactly what to draw from the bank at
// Gundai to pay for it.
type PurchasePlan struct {
	Lines []PlannedBuy
	Coins int
}

// Runes is the plan's total rune count.
func (p PurchasePlan) Runes() int {
	n := 0
	for _, l := range p.Lines {
		n += l.Count
	}
	return n
}

// Trip is the trip's own persisted state. It lives in the Checkpoint so a restart mid-Wilderness
// resumes on the right leg: position alone cannot tell "on the way in" from "on the way out".
type Trip struct {
	// Active is true from the moment the lever is pulled until the account is back in Ardougne.
	Active bool `json:"active"`
	// Bought is true once this trip's purchase is done (or found impossible): the account is now
	// heading OUT.
	Bought bool `json:"bought"`
	// PlannedCoins is what this trip's purchase will draw at Gundai.
	PlannedCoins int `json:"plannedCoins"`
	// Withdrawn is true once those coins are out of the bank inside the Mage Bank.
	Withdrawn bool `json:"withdrawn"`
}

// Phase3 is one step of Stage 3.
type Phase3 string

const (
	Phase3Wait     Phase3 = "wait"      // no tile yet
	Phase3Done     Phase3 = "done"      // the rune target is banked
	Phase3Return   Phase3 = "return"    // somewhere else (a Lumbridge respawn): go back to Ardougne
	Phase3Eat      Phase3 = "eat"       // thieving between trips and hurt
	Phase3Mule     Phase3 = "mule"      // runes/coins/staff wanted and the mule not asked lately: ask it first
	Phase3Staff    Phase3 = "staff"     // below the Magic target with no staff: get one
	Phase3Train    Phase3 = "train"     // below the Magic target with a staff and runes: autocast at the cows
	Phase3Market   Phase3 = "market"    // a placed market sells a wanted rune below Lundail: buy there
	Phase3Kit      Phase3 = "kit"       // in Ardougne, trip due: bank down to the exact kit
	Phase3Lever    Phase3 = "lever"     // kit verified: pull the Ardougne lever
	Phase3ToBank   Phase3 = "to-bank"   // Wilderness, inbound: walk, cut the webs, climb down
	Phase3Withdraw Phase3 = "withdraw"  // Mage Bank: draw the purchase coins at Gundai
	Phase3Buy      Phase3 = "buy"       // Mage Bank, coins drawn, not yet bought
	Phase3Deposit  Phase3 = "deposit"   // Mage Bank, bought: bank the runes and leftover coins
	Phase3ClimbOut Phase3 = "climb-out" // Mage Bank, pack holds only the blade
	Phase3ToKeep   Phase3 = "to-keep"   // Wilderness, outbound: walk to the Keep lever and pull
	Phase3Food     Phase3 = "food"      // back in Ardougne, no trip due: take food out to thieve
	Phase3Thieve   Phase3 = "thieve"    // waiting for Lundail to restock: pickpocket
	Phase3Aubury   Phase3 = "aubury"    // Lundail is dry: buy the wanted runes in Varrock instead of idling
	Phase3Guild    Phase3 = "guild"     // Magic 66+: buy the wanted runes at the Yanille Wizards' Guild store
	Phase3Peer     Phase3 = "peer"      // out of mind runes: take a graduate's leftovers at the Ardougne bank (mindshare.go)
	Phase3Hungry   Phase3 = "hungry"    // no food carried and none to be had yet: rest by the bank, never a pickpocket on an empty pack (hungry.go)
)

// Stage3Snapshot is the live state Stage 3 decides from.
type Stage3Snapshot struct {
	Tile  *tile.Tile
	HP    int
	MaxHP int
	// AtFarBase: the account trains at a Far Magic pen and stands on its home ground (magicpens.go
	// AtFarBase), which then counts as Stage 3's home instead of "elsewhere": the stage decides
	// there as it would in Ardougne, and its kit and food come from the pen's own bank.
	AtFarBase bool
	// Trip is the persisted trip state.
	Trip Trip
	// PackOnlyBlade is true when the pack/worn hold nothing but the blade (runes and coins are
	// banked): the condition for leaving the Mage Bank.
	PackOnlyBlade bool
	// TripDue is the supply decision: a purchase worth making is expected at Lundail now.
	TripDue bool
	// AuburyDue is true when a wanted rune can be bought from Aubury in Varrock and no
	// Wilderness trip is due — which is to say Lundail is too dry to be worth crossing for.
	//
	// WHY THIS EXISTS, measured on the live fleet 2026-09-22. Without it the account's answer
	// to "Lundail has nothing" was Phase3Thieve: pickpocket and wait for a shop that regrows
	// one mind rune every 50 ticks (120/hour) for the WHOLE WORLD, shared by the cohort. Two
	// pilot accounts spent 57 and 84 Wilderness crossings to buy 184 and 156 mind runes, while
	// each held over a million coins banked. Money was never the constraint; the shop was.
	//
	// Aubury sells the same mind rune from a stock of 1,000 regrowing one per 10 ticks
	// (600/hour) — 7x the standing stock and 5x the flow — and is in Varrock, with no
	// Wilderness to cross and nothing to die to. The fleet owner's instruction, 2026-09-22:
	// "they should be using Aubury in Varrock if the wilderness has no runes."
	//
	// It is deliberately NOT preferred over a due Lundail trip. Aubury is 400 tiles and two
	// ships from where the swarm lives (see SupplySettings.UseAubury), so it is the answer to
	// an EMPTY Lundail rather than a better shop in general.
	AuburyDue bool
	// GuildDue is true when the account's CURRENT Magic opens the Wizards' Guild (66) and a rune
	// it still wants has the guild as its first source, in a purchase worth the walk under the price
	// cap (GuildTripPlan in stage3supply.go; the runner adds the shop board's estimate and a failed
	// trip's backoff). The guild comes right after the mule:
	// no Wilderness, and 3000-deep elemental and mind lines against Lundail's 200 and 140.
	GuildDue bool
	// KitOK is CheckWildernessKit(...).OK: the blade and nothing else.
	KitOK bool
	// MuleAskDue is true when a mule is configured and has not been asked for these runes
	// recently: the stockpile comes before any shop (owner, 2026-09-19).
	MuleAskDue bool
	// MuleHoldsWant is whether the mule's published bank holds a rune the target still lacks.
	MuleHoldsWant bool
	// MuleHoldsStaff is whether the mule's published bank holds a staff on the ask ladder. A
	// staffless account then waits for its staff ask instead of thieving while Zaff is out of
	// reach (live 4 Oct 00:29: "could not reach Zaff without crossing White Wolf Mountain";
	// thieving and trying again in 10 minutes, with 562 Staffs of air at the mule).
	MuleHoldsStaff bool
	// MuleFoodAskDue is MuleAskDue WITHOUT the "the bank has been read once" gate.
	//
	// That gate exists so a first rune order is not sized against an unread bank (it once
	// asked for 360 runes for an account already holding over a thousand). It is right for
	// runes and WRONG for food, which is counted from the PACK and needs no bank reading at
	// all -- and the moment it matters most is the first arrival in Ardougne, when the bank
	// has by definition not been read yet. Without this split the mule-before-stall rule
	// below could never fire on the one visit that killed the account twice.
	MuleFoodAskDue bool
	// CoinsShort is true when the next purchase costs more than the bank's spendable coins —
	// with a mule, coins are asked for first (owner, 2026-09-19).
	CoinsShort bool
	// MagicLevel and MagicTarget drive training; HaveStaff is an autocast staff worn, carried or
	// banked; TrainCasts the training casts the runes on hand (pack + bank) pay for.
	MagicLevel  int
	MagicTarget int
	HaveStaff   bool
	TrainCasts  int
	// StaffWanted is true when the account will cast at ANY Magic level -- it is headed for
	// Stage 6 -- so a missing staff is fetched even past the Magic target (staffladder.go's
	// StaffNeeded; "scarabprod", 2026-09-23: the plain-Staff death livelock). False keeps the
	// old training-only rule.
	StaffWanted bool
	// StaffRetryWait is true while a staff hunt that found none waits out its retry (the
	// runner's ten minutes): Phase3Staff then only thieves, so it must keep thieving's food rules.
	StaffRetryWait bool
	// MarketDue is true when ChooseMarketBuy found a placed market cheaper than Lundail.
	MarketDue bool
	// TargetMet is true when the banked runes cover the stage's target.
	TargetMet bool
	// Food is edible food in the pack; FoodWanted how much the thieving rung wants carried.
	Food       int
	FoodWanted int
	// Stocking is true while the runner is part-way through a food restock. It is the
	// hysteresis that keeps the training branch's half-keep low-water mark from walking the
	// account back to the bank after every single bite: the restock STARTS at half the keep
	// and, once started, RUNS ON until the keep is full. Stage 5's Stage5Snapshot carries the
	// same field for the same stall and the same reason.
	Stocking bool
	// EatBelowHP is the eat threshold in HP, 0 to never eat.
	EatBelowHP int
	// ThieveStun is the damage one failed pickpocket does on the rung the account would thieve
	// now (thieverplan.Method.StunDamage of BestRungFor): it raises the thieving eat threshold so
	// two failures in a row cannot kill (ThieveEatBelowHP). 0 when unknown.
	ThieveStun int
	// LagTicks is the most ticks one view lag episode hid in the last gamestate.ViewLagMemory (gamestate.RecentLag.Ticks; 0 for
	// none), CombatLevel the account's combat level (0 unknown: the Watchman event is then assumed at its hardest) and LagFacing the
	// sum of the worst single hit of the NPCs facing the account, filled only while LagTicks is over 0. With a lag in the memory the
	// thieving and training eat thresholds are raised to what an episode could have hidden (lageat.go: ThieveEatBelowHP,
	// TrainEatBelowHP).
	LagTicks, CombatLevel, LagFacing int
	// LagReach is the sum of the worst single hit of everything that can hit the account where it stands, not only what faces it:
	// the attackable NPCs within three tiles count while it is being hit (lagescape-1018; the 18 Oct 15:14 death read "nothing faced the
	// account; near it: Ogre chieftain, Ogre"). It raises the training eat line (TrainEatBelowHP), so an account that has lagged lately
	// and stands among hitters eats before the next fight or cast instead of starting it. Filled with LagFacing; 0 for none.
	LagReach int
	// ThievingLevel gates the thieving-while-waiting rung.
	ThievingLevel int
	// PeerMindsDue is MindAskDue (mindshare.go): out of training runes, in the hand-over zone,
	// and a fleet account may be holding spare mind runes. Set by the runner; false when the
	// stage3MindShare setting is off.
	PeerMindsDue bool

	// The training ROUND (runesupply, 2026-09-25; trainround.go). MinTrainCasts is the smallest
	// round worth the walk to the training pen (stage3MinTrainCasts; 0 = any, the old rule);
	// CastsToTarget the casts the training spell still needs to the Magic target, or to the end
	// of the stocked round when that is sooner (0 = unknown), which a round never has to exceed;
	// Training is the hysteresis -- the account is part-way through a round and finishes what it
	// holds; WaitRungPaysTrainRune is true when the rung it thieves while waiting pays a rune the
	// training spell costs (a Paladin's chaos for Wind Bolt), so waiting GROWS the round rather
	// than leaving it where it is.
	MinTrainCasts         int
	CastsToTarget         int
	Training              bool
	WaitRungPaysTrainRune bool

	// TrainWithoutFight is true when the training step casts at nothing that hits back (an alch or
	// teleport mode, scarabswarm_trainmode.go): the no-food gate in front of a training fight does
	// not apply to it.
	TrainWithoutFight bool
}

// Stage3Region is where the account is, as Stage 3 cares about it.
type Stage3Region string

const (
	Region3Unknown    Stage3Region = "unknown"
	Region3Ardougne   Stage3Region = "ardougne"
	Region3Wilderness Stage3Region = "wilderness"
	Region3MageBank   Stage3Region = "mage-bank"
	Region3Elsewhere  Stage3Region = "elsewhere"
)

// RegionOf classifies a tile for Stage 3.
func RegionOf(t *tile.Tile) Stage3Region {
	if t == nil {
		return Region3Unknown
	}
	switch {
	case InMageBank(*t):
		return Region3MageBank
	case WildernessLevel(*t) > 0:
		return Region3Wilderness
	case NearArdougneLever(*t) || InArdougneReach(*t) || NearAnyPen(*t) || OnArdougneLowRung(*t): // every training pen and low pickpocket rung is home ground
		return Region3Ardougne
	}
	return Region3Elsewhere
}

// DecideStage3Phase is Stage 3's whole decision, re-derived from live state every Loop.
//
// The Wilderness legs come FIRST and ignore everything else — hunger included, because the kit
// carries no food by rule, and the only answer to danger up there is to keep moving toward the
// nearer exit (see WildernessHeading). Only in Ardougne does the stage choose between a trip and
// thieving while Lundail restocks.
func DecideStage3Phase(s Stage3Snapshot) Phase3 {
	switch RegionOf(s.Tile) {
	case Region3Unknown:
		return Phase3Wait
	case Region3MageBank:
		if !s.Trip.Bought {
			if !s.Trip.Withdrawn {
				return Phase3Withdraw
			}
			return Phase3Buy
		}
		if !s.PackOnlyBlade {
			return Phase3Deposit
		}
		return Phase3ClimbOut
	case Region3Wilderness:
		// An account in the Wilderness with no trip under way did not walk in: the server dropped it
		// there (Rogues' Castle at 04:35 on 2026-10-02, 237 accounts) or an ignored event did. Heading
		// "for the Mage Bank ladder" was the wrong answer twice over: the kit is not the blade-only
		// kit the trip's rule requires, and from the castle roof the walk was unreachable, so eight
		// accounts logged "could not reach 3158,3950 beside the web" every thirteen seconds for twelve
		// hours. Out by the Keep lever is what the trip's own way home does.
		if s.Trip.Bought || !s.Trip.Active {
			return Phase3ToKeep
		}
		return Phase3ToBank
	case Region3Elsewhere:
		if !s.AtFarBase {
			return Phase3Return
		}
	}
	// Ardougne.
	if s.TargetMet {
		return Phase3Done
	}
	training := s.MagicTarget > 0 && s.MagicLevel > 0 && s.MagicLevel < s.MagicTarget
	if (training || s.StaffWanted) && !s.HaveStaff {
		if s.MuleAskDue {
			return Phase3Mule
		}
		if s.MuleHoldsStaff {
			return Phase3Wait
		}
		// Waiting out a failed staff hunt is thieving, so it eats, restocks and rests like
		// thieving. Live 2026-09-26: a combat-3 account that could not reach Zaff pickpocketed
		// Warrior women on an empty pack from this branch, past the hungry rule below, and died
		// seven times in two hours. TestStage3StaffWaitKeepsTheFoodRules.
		if s.StaffRetryWait {
			if p, ok := thieveFoodPhase(s); ok {
				return p
			}
		}
		return Phase3Staff
	}
	// trainPhase is the training branch: eat, food, then cast. Taken here once the round is worth
	// the walk (TrainRoundReady), and again at the foot of the supply ladder when nothing else is
	// due and waiting would not grow the round (trainround.go).
	trainPhase := func() Phase3 {
		// The training eat line is the fight floor's (nodeaths.go): a fight broken off at the floor
		// is followed by a bite, never by the next fight.
		if eat := s.TrainEatBelowHP(); eat > 0 && s.HP > 0 && s.HP < eat && s.Food > 0 {
			return Phase3Eat
		}
		// NEVER A FIGHT ON AN EMPTY PACK (owner, 2026-09-26: "they must carry food"). This branch
		// used to train hungry once the food phase had come up short (FoodWanted 0), and 21 of the
		// 35 live deaths on 26b2816f were training fights. With no food: the mule when it is due,
		// the bank-then-stall food phase when it may run, otherwise Phase3Hungry, whose runner
		// thieves for food at the Baker's stall. TestStage3NeverTrainsOnAnEmptyPack.
		if s.Food <= 0 && !s.TrainWithoutFight {
			if s.MuleFoodAskDue {
				return Phase3Mule
			}
			if s.FoodWanted > 0 {
				return Phase3Food
			}
			return Phase3Hungry
		}
		// FOOD BEFORE TRAINING. Training is hand-casting at cows that retaliate, on an
		// account with the base ten hitpoints, so an empty pack is not a slow start — it is
		// a death, and each death costs a Lumbridge respawn and a cross-map walk back.
		//
		// Live-found 2026-09-22 (lab e2e `scarabe2e922`): SIX Stage 3 deaths, every one at
		// the Ardougne cow field 2660,3344, and every "training kit" line in that run read
		// "0 food". Phase3Food never ran once, because this branch returned before the food
		// gate below it could ever be reached; by the time an account had no casts left and
		// could fall through, TripDue had already claimed the loop. The phase that exists to
		// fetch food could therefore not fire while training, which is exactly when it is
		// needed. TestStage3WillNotTrainWithoutFood is that snapshot.
		//
		// The low-water mark is HALF the keep, not the keep itself: at "any shortfall" the
		// account would walk the 40 tiles back to the Ardougne bank after every single bite.
		// FoodWanted == 0 means the runner has already found no more food for now
		// (foodShortUntil): an account still carrying SOME trains on it rather than livelocking
		// on a phase that cannot help it; one carrying none never reaches here (above).
		if s.FoodWanted > 0 && (s.Food*2 <= s.FoodWanted || (s.Stocking && s.Food < s.FoodWanted)) {
			// THE MULE BEFORE THE STALL. The Baker's stall rouses guards that kill a
			// ten-hitpoint account in about four ticks, which is faster than this loop can
			// come round -- measured twice at 2668,3312, the second time on a build that
			// already had the retreat rule and still died with zero retreats. The mule is
			// free, posts at a bank rather than in a market, and is where the owner's own
			// supply order puts the first ask. The stall stays the fallback for an account
			// whose mule is empty or not yet due; it is simply no longer the FIRST answer.
			if s.MuleFoodAskDue {
				return Phase3Mule
			}
			return Phase3Food
		}
		return Phase3Train
	}
	if training && s.TrainCasts > 0 && TrainRoundReady(s) {
		return trainPhase()
	}
	// THE SUPPLY LADDER, in the fleet owner's own order (2026-09-22): "all gear and stock
	// should come from the mule if it has it, after the mule doesnt have any runes, shops
	// have no runes, then buy from the players around seers market. if theyve got some runes
	// and not all and all sources are getting more runes, then it should start to train magic
	// or theiving."
	//
	// So: MULE, then SHOPS, then PLAYERS, then train rather than idle. The order used to put
	// the player market ahead of both shops, which inverted two of those three steps — an
	// account would pay a player for a rune the mule was holding for free, and would pay a
	// player before trying a shop that had stock.
	//
	// Each step is "is there anything here for me", never "did this fail": a shop with no
	// stock and a mule with none both simply decline to be the answer, and the ladder falls
	// through. Nothing here retries a source that just said no.

	// 0. A graduate's leftover mind runes (the owner's 2026-09-23 design, mindshare.go): a local
	// trade at the Ardougne bank, before any walk to the mule or a shop. The runner's own choice
	// (peerplan.Choose) already falls back to the mule when no peer genuinely beats it.
	if training && s.PeerMindsDue {
		return Phase3Peer
	}
	// 1. The mule. Free, and its stockpile is the only source measured in tens of thousands.
	// MuleHoldsWant asks it whenever it holds a rune the account still lacks, not only when a
	// shop trip happens to be due: live 3 Oct, with 935k chaos in the mule's bank, Stage 3 logged
	// "thieving while Lundail restocks" 1,132 times because Lundail's low stock left TripDue off.
	if (s.TripDue || s.CoinsShort || s.MarketDue || s.AuburyDue || s.GuildDue || s.MuleHoldsWant) && s.MuleAskDue {
		return Phase3Mule
	}
	// 1b. The Wizards' Guild store in Yanille, from Magic 66 (a user request: "make sure when we
	// hit 66 magic the wizards guild is considered as a place to buy runes from"). A shop, so ahead
	// of the player market in the owner's order; ahead of Lundail because it needs no Wilderness
	// crossing and holds fifteen times her stock. GuildDue is false below 66 and for any rune whose
	// first source is a pickpocket rung, so nothing changes for an account that cannot use it.
	if s.GuildDue {
		return Phase3Guild
	}
	// 2. A placed player market, but ONLY when it beats the shop. This looks like it
	// contradicts the owner's stated order ("after the mule doesnt have any runes, shops
	// have no runes, then buy from the players around seers market") and does not, because
	// MarketDue is not "a player is selling": ChooseMarketBuy sets it only when a placed
	// market is CHEAPER than Lundail, or Lundail is depleted (its own test: "a depleted
	// Lundail should lose to a fair market"). So the intent behind that order — never pay a
	// player for something a free or stocked source has — is already satisfied here, and
	// moving this step below the shops would make an account pay MORE at Lundail while a
	// player sold the same rune for less.
	//
	// Tried and reverted 2026-09-22: moving this below Aubury broke
	// TestDecideStage3PhaseTrainsFirst's "staff, no runes, market cheaper" case, which is
	// exactly the case the price rule exists for.
	if s.MarketDue {
		return Phase3Market
	}
	// 3a. Lundail at the Mage Bank, when her stock is worth the Wilderness crossing.
	if s.TripDue {
		// A PLANNED PURCHASE IS PART OF BEING READY, not a separate question, because the
		// decision to GO and the amount to WITHDRAW come from two different places that can
		// disagree.
		//
		// TripDue is recomputed from a FRESH plan every snapshot, so it turns true again as
		// soon as Lundail's estimated stock regrows. Trip.PlannedCoins is a STORED figure
		// that only Phase3Kit ever writes, and it is reset to 0 when a trip comes home. KitOK
		// means "carries a blade and nothing else", which is exactly the state a trip comes
		// home in -- so the kit pass was skipped, PlannedCoins stayed 0, and the account
		// pulled the lever on a fresh plan while Gundai drew nothing.
		//
		// The account was NOT broke, which is the part worth remembering: the mule had given
		// it 3,000 coins and it had spent 822. It crossed the Wilderness ELEVEN times with an
		// EMPTY PURSE and roughly 2,178 coins sitting in the bank. Buying nothing also left
		// the pack at just the blade, so Phase3Deposit was skipped and nothing in the cycle
		// ever re-read the bank. Measured live 2026-09-22 (run b5g115r7t).
		// See TestStage3WillNotCrossTheWildernessToBuyNothing.
		if s.KitOK && s.Trip.PlannedCoins > 0 {
			return Phase3Lever
		}
		return Phase3Kit
	}
	// 3b. Aubury in Varrock. Further away but seven times the standing stock and five times
	// the restock rate, with no Wilderness to cross — so it is the answer to an EMPTY
	// Lundail rather than a better shop in general. This is the step that did not exist
	// before 2026-09-22, and its absence is what made two pilot accounts spend 57 and 84
	// Wilderness crossings for ~170 mind runes while holding a million coins each.
	if s.AuburyDue {
		return Phase3Aubury
	}
	// A round under the minimum, and no source above is due: cast what is held rather than idle
	// on it (the owner's "train rather than idle") -- UNLESS the wait rung itself pays the
	// training rune, when thieving on grows the round and the walk is made once, full.
	if training && s.TrainCasts > 0 && (!s.WaitRungPaysTrainRune || s.MuleHoldsWant) {
		return trainPhase()
	}
	// NO THIEVING FOR WHAT THE MULE HOLDS (a user request: "no accounts should be thieving
	// paladins for coins and chaos as the mule has both"; the mule held 52.8M coins and 862k chaos while
	// Stage 3 logged "thieving while Lundail restocks" 608 times). With nothing to cast the account
	// waits for its next mule ask (stage3MuleAskDue shortens it to five minutes in this case).
	if s.MuleHoldsWant {
		return Phase3Wait
	}
	if p, ok := thieveFoodPhase(s); ok {
		return p
	}
	return Phase3Thieve
}

// thieveFoodPhase is the food gate in front of every pickpocket: eat, restock, or rest when the
// pack is empty. ok false means the account may thieve.
func thieveFoodPhase(s Stage3Snapshot) (Phase3, bool) {
	// The thieving eat threshold is the rung's, not only the setting's: see ThieveEatBelowHP.
	if eat := s.ThieveEatBelowHP(); eat > 0 && s.HP > 0 && s.HP < eat && s.Food > 0 {
		return Phase3Eat, true
	}
	// The same half-keep low-water mark and Stocking hysteresis the training branch uses, and
	// for the same reason. This gate predates the Baker's-stall fallback, when the phase could
	// only move ALREADY-BANKED food and a needless entry cost nothing; now that the phase
	// actually walks to the bank and then on to the stall, "7 food of 8 wanted" sends the
	// account on a 29-tile round trip for ONE cake. Live-measured 2026-09-22 (run b5g115r7t):
	// once Magic reached its target the account stopped taking the training branch, fell
	// through to here, and logged "the bank gave 7 food of 8 wanted: stocking at the Baker's
	// stall" followed by "stocked 8 food at the stall in 13s (1 steals)".
	if s.FoodWanted > 0 && (s.Food*2 <= s.FoodWanted || (s.Stocking && s.Food < s.FoodWanted)) {
		// Same rule as the training gate above: ask the stocked mule before walking
		// into the guards. See TestStage3AsksTheMuleForFoodBeforeRiskingTheStall.
		if s.MuleFoodAskDue {
			return Phase3Mule, true
		}
		return Phase3Food, true
	}
	// NEVER A PICKPOCKET ON AN EMPTY PACK (owner, 2026-09-25: "the accounts shouldn't die if they
	// have food, mule holds plenty"). Reached with no food only when the food phase has just come
	// up short (FoodWanted 0: the bank was empty and the mule not askable yet), and every rung on
	// the ladder hits back -- a Guard 2 a failure, a Knight 3 -- so thieving on here was death on
	// a timer. See hungry.go for the measured deaths and TestStage3NeverThievesOnAnEmptyPack.
	if s.Food <= 0 {
		return Phase3Hungry, true
	}
	return "", false
}

// WildernessHeading is where an account in the Wilderness should be walking: the Mage Bank
// ladder on the way in, the Keep lever on the way out — EXCEPT under attack, when it is whichever
// of the two is nearer, because either one ends the danger (the ladder drops into a non-Wilderness
// bank, the lever teleports out) and the account carries no food by rule. This is the content's
// own answer to "flee toward the Mage Bank or the lever".
func WildernessHeading(here tile.Tile, bought, underAttack bool) tile.Tile {
	if underAttack {
		if tile.Chebyshev(here, MageBankLadder) <= tile.Chebyshev(here, KeepLever) {
			return MageBankLadder
		}
		return KeepLever
	}
	if bought {
		return KeepLever
	}
	return MageBankLadder
}
