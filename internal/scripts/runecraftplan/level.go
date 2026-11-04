package runecraftplan

import (
	"strconv"
	"strings"
	"time"
)

// Source is where a levelling round gets its essence (craftRuneEssence).
type Source string

// The sources.
const (
	// SourceAuto is the ladder: this account's bank, then the mule's published stock, then mining, then a bounded
	// market lot (craftRuneBuyBudget, off by default).
	SourceAuto Source = "auto"
	// SourceBank uses only the account's own bank, SourceMule only the mule's stock, SourceMine only the
	// account's own mining at the Wizards' Guild (or the gateway chosen).
	SourceBank Source = "bank"
	SourceMule Source = "mule"
	SourceMine Source = "mine"
)

// ValidSource reports whether s is one of the four.
func ValidSource(s Source) bool {
	switch s {
	case SourceAuto, SourceBank, SourceMule, SourceMine:
		return true
	}
	return false
}

// Stage is the next thing a levelling round does.
type Stage string

// The stages.
const (
	// StageDone ends the round: the target level is reached.
	StageDone Stage = "done"
	// StageCraft binds the essence in the pack at the altar the account stands in.
	StageCraft Stage = "craft"
	// StagePortalOut leaves the temple by its portal.
	StagePortalOut Stage = "portal-out"
	// StageWalkToRuins walks onto a tile the engine accepts a talisman on the ruins from.
	StageWalkToRuins Stage = "walk-to-ruins"
	// StageUseTalisman uses the talisman on the ruins and waits to be seen inside the temple.
	StageUseTalisman Stage = "use-talisman"
	// StageBank is a bank visit: bank what is carried (the talisman stays), read the bank, draw the
	// talisman if it is not in hand and the essence.
	StageBank Stage = "bank"
	// StageAskMule asks the mule for Lines at its post (talismans, essence, the teleport kit) and banks
	// what arrives.
	StageAskMule Stage = "ask-mule"
	// StageMine is a mining stretch: essence mined and banked until Goal more is in the bank.
	StageMine Stage = "mine"
	// StageBuy buys a bounded lot of essence at the Seers stalls (craftRuneBuyBudget) and banks it: the last rung of
	// the ladder, after the mule and mining.
	StageBuy Stage = "buy"
	// StageStuck ends the round for a reason that will not pass at once (no essence anywhere, no
	// talisman, a members rune on a free world).
	StageStuck Stage = "stuck"
)

// Line is one line of an ask to the mule.
type Line struct {
	Item string
	Qty  int
}

// Kit is the runes of the travel teleports: Falador Teleport (1 Law, 1 Water, 3 Air) out to the altars
// and Camelot Teleport (1 Law, 5 Air) home, both cast by scarabrunner.WalkTo's teleport travel when
// the pack holds the runes and the walk is long.
type Kit struct {
	Law   int `json:"law,omitempty"`
	Water int `json:"water,omitempty"`
	Air   int `json:"air,omitempty"`
}

// The kit's sizes: what a bank visit before a long walk draws (two casts of each, so a refused first
// cast leaves one for the retry), what the bank must hold before an ask tops it up, and what the ask
// tops it up to (ten rounds out and home).
var (
	KitDraw   = Kit{Law: 2, Water: 2, Air: 10}
	KitMin    = Kit{Law: 4, Water: 2, Air: 16}
	KitTarget = Kit{Law: 20, Water: 10, Air: 80}
)

// Names of the kit's runes as the bank and the mule publish them.
const (
	RuneLaw   = "Law rune"
	RuneWater = "Water rune"
	RuneAir   = "Air rune"
)

// Slots is how many pack slots the kit takes (a rune stack each).
func (k Kit) Slots() int {
	n := 0
	for _, v := range []int{k.Law, k.Water, k.Air} {
		if v > 0 {
			n++
		}
	}
	return n
}

// Covers reports whether k holds at least what min asks of each rune.
func (k Kit) Covers(min Kit) bool {
	return k.Law >= min.Law && k.Water >= min.Water && k.Air >= min.Air
}

// KitStock is a Kit read from a stock count by rune name.
func KitStock(count func(item string) int) Kit {
	return Kit{Law: count(RuneLaw), Water: count(RuneWater), Air: count(RuneAir)}
}

// KitAskGap is the least time between two kit-only asks of the mule (an ask for nothing but the teleport kit's runes). The kit is a
// convenience, not a need: a round that finds the bank's short of it asks at most this often, and not at all when the ask could not
// make the kit usable (kitCompletes).
const KitAskGap = 2 * time.Hour

// Tuning of the asks.
const (
	// TalismanAsk is how many of a talisman one ask brings (they are not consumed: one is enough, two
	// cover a death).
	TalismanAsk = 2
	// MinMuleEssence is the fewest essence worth an ask: one load.
	MinMuleEssence = LoadSize
)

// Snapshot is what DecideLevelStage reads.
type Snapshot struct {
	// Level and XP are the Runecrafting stat as the server last sent it; Target the level the plan
	// stops at.
	Level, XP, Target int
	// Members is whether the world crafts members runes (the setting and the pack's flag).
	Members bool
	// Where is the account's region; Temple names the altar's key when it is in one.
	Where  Region
	Temple string
	// AtRuins is set when the account stands where the engine accepts a talisman on the ruins of the
	// altar PickAltar chooses.
	AtRuins bool
	// AtPost is set when the account is by the mule's post (within a short walk of it).
	AtPost bool
	// PackEssence is the unnoted essence in the pack; PackTalisman the talismans carried, by altar key.
	PackEssence  int
	PackTalisman map[string]bool
	// BankKnown is set when the account's bank has been read (this session, or remembered by the
	// ledger); BankEssence and BankTalisman what it holds, BankKit the teleport runes in it.
	BankKnown    bool
	BankEssence  int
	BankTalisman map[string]int
	BankKit      Kit
	// MuleKnown is set when the mule's published bank is on hand; the rest is what it holds.
	MuleKnown    bool
	MuleEssence  int
	MuleTalisman map[string]int
	MuleKit      Kit
	// Source is craftRuneEssence.
	Source Source
	// CanMine says the account can mine essence (the gateway's Magic, Rune Mysteries, a pickaxe) and
	// MineWhy why not when it cannot; MineFailed says a stretch has failed this round.
	CanMine    bool
	MineWhy    string
	MineFailed bool
	// Batch is craftRuneBatch: the essence one mining stretch brings.
	Batch int
	// CanBuy says a bounded market lot is possible now (craftRuneBuyBudget, the mule's coins above its floor, a world
	// with the stalls); BuyQty is the most essence it may buy and BuyWhy why there is no lot; BuyFailed says a lot has
	// failed this round.
	CanBuy    bool
	BuyQty    int
	BuyWhy    string
	BuyFailed bool
	// Asked is set once the mule has been asked this round (it is asked once: what it declined stays
	// declined).
	Asked bool
	// KitWanted is set when travel teleports are on and Magic allows Falador Teleport.
	KitWanted bool
	// KitAskRecent is set when a kit-only ask of the mule was made less than KitAskGap ago (answered or not).
	KitAskRecent bool
}

// Step is DecideLevelStage's answer.
type Step struct {
	Stage Stage
	// Altar is the altar the stage works at (PickAltar's, or the temple the account stands in).
	Altar Altar
	// Lines is a StageAskMule's ask; Goal a StageMine's essence to bring into the bank.
	Lines []Line
	Goal  int
	// Optional marks a StageAskMule whose failure is no failure: the ask is the teleport kit alone, which the round does without
	// (on foot it is 400 steps). The handler never fails the round for it and does not ask again this round.
	Optional bool
	// Why is the one-line reason, for the log.
	Why string
}

// PickAltar is the altar for level: Air under 20, Body from 20, Nature from 44. have, when not nil, says
// which talismans the account can lay hands on (only Nature's is checked: Air's and Body's are plentiful
// and the mule is asked for them). ok is false with a reason for a members rune on a free world or a
// Nature craft without its talisman.
func PickAltar(level int, members bool, have map[string]bool) (a Altar, ok bool, why string) {
	a = Air
	for _, c := range Altars() {
		if level >= c.Level {
			a = c
		}
	}
	if a.Members && !members {
		return a, false, "this is not a members world: " + a.Name + " runes are a members rune and the craft does nothing here"
	}
	if a.Key == KeyNature && have != nil && !have[KeyNature] {
		return a, false, "no Nature talisman is held (the fleet has seven, all in the mule's bank)"
	}
	return a, true, ""
}

// Remaining is the essence the plan from the account's experience to its target still needs.
func (s Snapshot) Remaining() int {
	xp := max(s.XP, XPForLevel(max(1, s.Level)))
	return BudgetFrom(xp, s.Target, s.Members).Total
}

func (s Snapshot) held(key string) bool { return s.PackTalisman[key] }

// goThreshold is the essence a pack must hold to go to the ruins: a load, near enough (MinGo) when the
// bank has more to give, any when it has not.
func (s Snapshot) goThreshold() int {
	if s.BankKnown && s.BankEssence <= 0 {
		return 1
	}
	return MinGo
}

// kitShort reports whether the teleport kit in the bank is under KitMin.
func (s Snapshot) kitShort() bool { return s.KitWanted && !s.BankKit.Covers(KitMin) }

// DecideLevelStage chooses the next stage from where the account stands and what it holds. It is a
// function of the live state alone, so a restart anywhere, in the temple, in the mine, on the road with a
// load, resumes in the right place; a stage that fails is simply decided again.
//
//   - inside a temple: craft what is in the pack (if the altar's level allows), else leave by the portal;
//   - inside the mine: the mining stretch carries on;
//   - outside, at the target: done;
//   - outside with a load and the talisman: to the ruins and the talisman on them;
//   - otherwise provision: read the bank; draw when it holds the talisman and a trip's essence; else ask
//     the mule (once a round), else mine, else stop and say what is missing.
func DecideLevelStage(s Snapshot) Step {
	switch s.Where {
	case RegionTemple:
		a, ok := ByKey(s.Temple)
		if !ok {
			return Step{Stage: StageStuck, Why: "standing in a temple that is not in the table (" + s.Temple + ")"}
		}
		if s.PackEssence > 0 && s.Level >= a.Level && (!a.Members || s.Members) {
			return Step{Stage: StageCraft, Altar: a, Why: Thousands(s.PackEssence) + " essence in the pack at the " + a.Name + " altar"}
		}
		why := "no essence left in the pack"
		switch {
		case s.PackEssence > 0 && s.Level < a.Level:
			why = "Runecrafting " + strconv.Itoa(s.Level) + " is under the " + strconv.Itoa(a.Level) + " the " + a.Name + " altar needs"
		case s.PackEssence > 0:
			why = "the " + a.Name + " altar needs a members world"
		}
		return Step{Stage: StagePortalOut, Altar: a, Why: why}
	case RegionMine:
		return Step{Stage: StageMine, Goal: s.mineGoal(0), Why: "standing in the essence mine"}
	}
	if s.Level >= s.Target {
		return Step{Stage: StageDone, Why: "Runecrafting " + strconv.Itoa(s.Level) + " has reached " + strconv.Itoa(s.Target)}
	}
	altar, ok, why := PickAltar(s.Level, s.Members, nil)
	if !ok {
		return Step{Stage: StageStuck, Altar: altar, Why: why}
	}
	key := altar.Key
	haveTal := s.held(key)
	if s.PackEssence >= s.goThreshold() && haveTal {
		if s.AtRuins {
			return Step{Stage: StageUseTalisman, Altar: altar, Why: Thousands(s.PackEssence) + " essence and the " + altar.Talisman + " in hand, at the ruins"}
		}
		return Step{Stage: StageWalkToRuins, Altar: altar, Why: Thousands(s.PackEssence) + " essence and the " + altar.Talisman + " in hand"}
	}
	if !s.BankKnown {
		return Step{Stage: StageBank, Altar: altar, Why: "the bank has not been read yet"}
	}
	needTal := !haveTal
	talOK := !needTal || s.BankTalisman[key] > 0
	have := s.PackEssence + s.BankEssence
	essOK := have >= MinGo
	if talOK && essOK {
		// The kit is topped up on the way out when the post is here, the mule has runes for it, it has not been asked lately and the
		// ask would make the kit usable: a convenience, so a trade is spent on it only when it is worth one, and its failure is
		// not the round's (Step.Optional).
		if s.AtPost && !s.Asked && !s.KitAskRecent && s.kitShort() {
			if lines := s.kitLines(); len(lines) > 0 && s.kitCompletes(lines) {
				return Step{Stage: StageAskMule, Altar: altar, Lines: lines, Optional: true, Why: "the teleport kit in the bank is short (" + s.kitWord() + ")"}
			}
		}
		return Step{Stage: StageBank, Altar: altar, Why: "draw the " + altar.Talisman + " and a load: " + Thousands(s.BankEssence) + " essence in the bank"}
	}
	// Something is missing: the mule, then mining.
	if !s.Asked {
		if lines := s.askLines(altar, !essOK, !talOK); len(lines) > 0 {
			return Step{Stage: StageAskMule, Altar: altar, Lines: lines, Why: s.missingWord(altar, !essOK, !talOK)}
		}
	}
	// Mining is pointless without the talisman to use the essence with.
	if !talOK {
		return Step{Stage: StageStuck, Altar: altar, Why: "no " + altar.Talisman + " in the pack or the bank, and the mule's published bank holds " + strconv.Itoa(s.MuleTalisman[key]) + " (asked: " + yesNo(s.Asked) + ")"}
	}
	if s.Source != SourceBank && s.Source != SourceMule && s.CanMine && !s.MineFailed {
		return Step{Stage: StageMine, Altar: altar, Goal: s.mineGoal(have), Why: s.missingWord(altar, true, false) + ": mining"}
	}
	// The last rung: a bounded lot at the Seers stalls, only on the ladder (not when the owner named one source).
	if s.Source == SourceAuto && s.CanBuy && !s.BuyFailed && s.BuyQty >= MinGo {
		return Step{Stage: StageBuy, Altar: altar, Goal: s.BuyQty, Why: s.missingWord(altar, true, false) + ": a market lot of up to " + Thousands(s.BuyQty) + " essence"}
	}
	return Step{Stage: StageStuck, Altar: altar, Why: "no essence source: " + s.noEssenceWord(have)}
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// mineGoal is how many essence the next mining stretch brings into the bank: a batch, no more than
// the plan still needs (and at least a load).
func (s Snapshot) mineGoal(have int) int {
	batch := s.Batch
	if batch < LoadSize {
		batch = 1000
	}
	need := s.Remaining() - have
	goal := min(batch, need)
	if goal < LoadSize {
		goal = LoadSize
	}
	// whole loads
	return (goal + LoadSize - 1) / LoadSize * LoadSize
}

func (s Snapshot) kitWord() string {
	return strconv.Itoa(s.BankKit.Law) + " Law, " + strconv.Itoa(s.BankKit.Water) + " Water, " + strconv.Itoa(s.BankKit.Air) + " Air"
}

func (s Snapshot) missingWord(a Altar, essence, talisman bool) string {
	var miss []string
	if essence {
		miss = append(miss, "essence (bank "+Thousands(s.BankEssence)+", pack "+Thousands(s.PackEssence)+")")
	}
	if talisman {
		miss = append(miss, "the "+a.Talisman)
	}
	return strings.Join(miss, " and ") + " missing"
}

func (s Snapshot) noEssenceWord(have int) string {
	parts := []string{"own bank and pack " + Thousands(have)}
	if s.MuleKnown {
		parts = append(parts, "mule "+Thousands(s.MuleEssence))
	} else {
		parts = append(parts, "mule's bank not published")
	}
	switch {
	case s.Source == SourceBank || s.Source == SourceMule:
		parts = append(parts, "mining off (craftRuneEssence "+string(s.Source)+")")
	case s.MineFailed:
		parts = append(parts, "mining failed this round")
	case !s.CanMine:
		why := s.MineWhy
		if why == "" {
			why = "not possible"
		}
		parts = append(parts, "mining: "+why)
	}
	if s.Source == SourceAuto {
		switch {
		case s.BuyFailed:
			parts = append(parts, "the market lot failed this round")
		case !s.CanBuy:
			why := s.BuyWhy
			if why == "" {
				why = "off"
			}
			parts = append(parts, "market lot: "+why)
		default:
			parts = append(parts, "market lot of "+Thousands(s.BuyQty)+" is too small to go for")
		}
	}
	return strings.Join(parts, "; ")
}

// askLines is the ask for what the round is missing, plus the talismans the rest of the plan will want
// and the teleport kit when it is short. Nothing the mule does not publish is asked for.
func (s Snapshot) askLines(current Altar, needEssence, needTalisman bool) []Line {
	if !s.MuleKnown {
		return nil
	}
	var lines []Line
	if needEssence && s.Source != SourceBank && s.Source != SourceMine && s.MuleEssence >= MinMuleEssence {
		want := s.Remaining() - s.PackEssence - s.BankEssence
		if q := min(s.MuleEssence, want); q >= MinMuleEssence {
			lines = append(lines, Line{Item: EssenceName, Qty: q})
		} else if want >= MinGo && s.MuleEssence >= want {
			lines = append(lines, Line{Item: EssenceName, Qty: want})
		}
	}
	if needTalisman || len(lines) > 0 {
		// Every talisman the plan will use that the bank lacks, so Body's comes with Air's.
		budget := BudgetFrom(max(s.XP, XPForLevel(max(1, s.Level))), s.Target, s.Members)
		for _, seg := range budget.Segments {
			a := seg.Altar
			if s.held(a.Key) || s.BankTalisman[a.Key] > 0 {
				continue
			}
			if s.MuleTalisman[a.Key] >= TalismanAsk {
				lines = append(lines, Line{Item: a.Talisman, Qty: TalismanAsk})
			} else if s.MuleTalisman[a.Key] > 0 {
				lines = append(lines, Line{Item: a.Talisman, Qty: s.MuleTalisman[a.Key]})
			}
		}
	}
	if len(lines) > 0 {
		lines = append(lines, s.kitLines()...)
	}
	return lines
}

// kitLines tops the bank's teleport kit up to KitTarget from what the mule holds, when the kit is
// wanted and short.
func (s Snapshot) kitLines() []Line {
	if !s.kitShort() || !s.MuleKnown {
		return nil
	}
	var out []Line
	add := func(item string, have, target, mule int) {
		if q := min(target-have, mule); q > 0 {
			out = append(out, Line{Item: item, Qty: q})
		}
	}
	add(RuneLaw, s.BankKit.Law, KitTarget.Law, s.MuleKit.Law)
	add(RuneWater, s.BankKit.Water, KitTarget.Water, s.MuleKit.Water)
	add(RuneAir, s.BankKit.Air, KitTarget.Air, s.MuleKit.Air)
	return out
}

// kitCompletes reports whether the bank's kit plus these lines covers KitMin in every rune: a top-up that leaves a rune short (the
// mule holds no Water, or one) would be asked again at the next round and still draw nothing (KitNeeded wants a Law, a Water and
// five Air in the bank), so it is not worth a trade.
func (s Snapshot) kitCompletes(lines []Line) bool {
	post := s.BankKit
	for _, l := range lines {
		switch l.Item {
		case RuneLaw:
			post.Law += l.Qty
		case RuneWater:
			post.Water += l.Qty
		case RuneAir:
			post.Air += l.Qty
		}
	}
	return post.Covers(KitMin)
}

// KitNeeded reports whether a bank visit before a walk should draw the teleport kit: it is wanted,
// the walk from here to the next destination is at least minSave tiles in a straight line, and the bank
// holds a draw's worth.
func KitNeeded(wanted bool, straightTiles, minSave int, bank Kit) bool {
	return wanted && straightTiles >= minSave && bank.Covers(Kit{Law: 1, Water: 1, Air: 5})
}
