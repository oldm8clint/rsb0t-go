package crafterplan

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/scripts/donorplan"
	"rsb0t-go/internal/scripts/freshstart"
	"rsb0t-go/internal/scripts/levels"
	"rsb0t-go/internal/scripts/tile"
)

// Settings are ProgressiveCrafter's settings. The JSON keys are the TS manifest's, so an account's
// fleet.config.json settings object decodes straight into DefaultSettings().
type Settings struct {
	GuideNPC         string    `json:"guideNpc"`
	SkipOption       string    `json:"skipOption"`
	Fare             int       `json:"fare"`
	DropAt           int       `json:"dropAt"`
	PickpocketTarget string    `json:"pickpocketTarget"`
	PickpocketTile   tile.Tile `json:"pickpocketTile"`
	// RestBelowHP is a percentage of maximum health.
	RestBelowHP int       `json:"restBelowHp"`
	SkirtCost   int       `json:"skirtCost"`
	StoreStand  tile.Tile `json:"storeStand"`
	PenTile     tile.Tile `json:"penTile"`
	WheelTile   tile.Tile `json:"wheelTile"`
	FlaxSource  string    `json:"flaxSource"`
	// FlaxKitSize is the flax one mule flax kit asks for (flaxSource MuleFlax only).
	FlaxKitSize int `json:"flaxKitSize"`
	// FlaxKitBackoffMinutes is the wait after a flax kit that brought nothing, doubled for each
	// fruitless kit in a row (flaxSource MuleFlax only).
	FlaxKitBackoffMinutes int `json:"flaxKitBackoffMinutes"`
	// Mule is the MuleReceiver account; blank never hands anything over.
	Mule string `json:"mule"`
	// MuleAt is the banked bow string count that starts a hand-over.
	MuleAt int `json:"muleAt"`
	// MuleKeep is a comma-separated list of names that stay in the bank.
	MuleKeep  string `json:"muleKeep"`
	LogDetail string `json:"logDetail"`
}

// DefaultSettings are the manifest defaults. The mule defaults to mulecon (the TS manifest writes
// Mulecon; names compare case-insensitively).
func DefaultSettings() Settings {
	return Settings{
		GuideNPC:              freshstart.DefaultGuideNPC,
		SkipOption:            freshstart.DefaultSkipOption,
		Fare:                  freshstart.DefaultFare,
		DropAt:                freshstart.DefaultDropAt,
		PickpocketTarget:      freshstart.DefaultPickpocketTarget,
		PickpocketTile:        freshstart.PickpocketSpot,
		RestBelowHP:           40,
		SkirtCost:             freshstart.DefaultSkirtCost,
		StoreStand:            freshstart.StoreStand,
		PenTile:               freshstart.LumbridgePen,
		WheelTile:             freshstart.LumbridgeWheel,
		FlaxSource:            PickFlax,
		FlaxKitSize:           DefaultFlaxKitSize,
		FlaxKitBackoffMinutes: DefaultFlaxKitBackoffMinutes,
		Mule:                  donorplan.DefaultMule,
		MuleAt:                donorplan.DefaultMuleAt,
		MuleKeep:              donorplan.DefaultKeep,
		LogDetail:             "Normal",
	}
}

// Normalised applies the TS setting readers' fallbacks and bounds.
func (s Settings) Normalised() Settings {
	d := DefaultSettings()
	if strings.TrimSpace(s.GuideNPC) == "" {
		s.GuideNPC = d.GuideNPC
	}
	if strings.TrimSpace(s.SkipOption) == "" {
		s.SkipOption = d.SkipOption
	}
	if strings.TrimSpace(s.PickpocketTarget) == "" {
		s.PickpocketTarget = d.PickpocketTarget
	}
	s.GuideNPC, s.SkipOption, s.PickpocketTarget = strings.TrimSpace(s.GuideNPC), strings.TrimSpace(s.SkipOption), strings.TrimSpace(s.PickpocketTarget)
	if s.Fare < 0 {
		s.Fare = 0
	}
	if s.DropAt < 1 {
		s.DropAt = 1
	}
	if s.SkirtCost < 0 {
		s.SkirtCost = 0
	}
	if s.MuleAt < 0 {
		s.MuleAt = 0
	}
	s.FlaxKitSize = ClampFlaxKitSize(s.FlaxKitSize)
	if s.FlaxKitBackoffMinutes < 1 {
		s.FlaxKitBackoffMinutes = 1
	}
	if limit := int(MaxFlaxKitBackoff / time.Minute); s.FlaxKitBackoffMinutes > limit {
		s.FlaxKitBackoffMinutes = limit
	}
	if s.MuleAt > donorplan.MaxMuleAt {
		s.MuleAt = donorplan.MaxMuleAt
	}
	return s
}

// RestBelow is the health share to rest at, clamped to 5% through 90%.
func (s Settings) RestBelow() float64 {
	f := float64(s.RestBelowHP) / 100
	if f < 0.05 {
		return 0.05
	}
	if f > 0.9 {
		return 0.9
	}
	return f
}

// Verbose reports the Verbose log detail.
func (s Settings) Verbose() bool {
	return strings.EqualFold(strings.TrimSpace(s.LogDetail), "verbose")
}

// Donor is the hand-over settings, with banked flax kept while it is the run's stock.
func (s Settings) Donor() donorplan.Settings {
	d := donorplan.NewSettings(s.Mule, s.MuleAt, s.MuleKeep)
	d.Keep = MuleKeep(s)
	return d
}

// StoreKeep names the store never sees: the kit, and the wool it will not pay for.
func StoreKeep() []string {
	return []string{freshstart.Coins, freshstart.Shears, freshstart.PinkSkirt, freshstart.Wool, freshstart.Ball}
}

// SeersKeep names what stays in the pack at the Seers bank.
func SeersKeep() []string {
	return []string{freshstart.Coins}
}

// MuleKeep is the bank rows that stay when the rest goes to the mule: the keep setting, plus flax
// while the run draws its flax from the bank.
func MuleKeep(s Settings) []string {
	keep := donorplan.ParseList(s.MuleKeep)
	if KeepsBankFlax(s.FlaxSource) {
		keep = append(keep, Flax)
	}
	return keep
}

// PackKeep is what stays in the pack during a hand-over: the Seers kit plus the keep list.
func PackKeep(s Settings) []string {
	return append(SeersKeep(), MuleKeep(s)...)
}

func isBowString(row donorplan.ItemRow) bool {
	return strings.EqualFold(strings.TrimSpace(row.Name), BowString)
}

// MuleRules are the hand-over rules: bow strings first, the keep list stays. Set Untradeable on
// the result to keep items the game will not trade.
func MuleRules(keep []string) donorplan.Rules {
	return donorplan.Rules{Keep: keep, First: isBowString}
}

// MuleRows is what leaves the Seers bank for the mule: bow strings first, then everything not kept.
func MuleRows(bank []donorplan.ItemRow, keep []string) []donorplan.ItemRow {
	return donorplan.HandoffRows(bank, MuleRules(keep))
}

// MuleHaul counts pack slots holding something for the mule.
func MuleHaul(pack []donorplan.ItemRow, keep []string) int {
	return donorplan.HaulOf(pack, MuleRules(keep))
}

// MuleLoad is what goes on the trade screen: bow strings first, cut to the mule's free slots.
func MuleLoad(pack []donorplan.ItemRow, freeSlots int, keep []string) []donorplan.LoadGroup {
	return donorplan.PlanLoad(pack, freeSlots, MuleRules(keep))
}

// HandoffAfterBanking reports whether banking bow strings should start a hand-over. stringsInBank
// is nil until a bank visit has counted them.
func HandoffAfterBanking(s Settings, stringsInBank *int, now, nextAllowed time.Time) bool {
	d := s.Donor()
	if !d.Enabled() {
		return false
	}
	banked := 0
	if stringsInBank != nil {
		banked = *stringsInBank
	}
	return donorplan.HandoffDue(banked, d.MuleAt, now, nextAllowed)
}

// DrawFlaxWithStrings reports whether the strings round should draw a pack of flax in the SAME bank visit (twopertick-1012:
// it used to bank the strings, close the bank and come straight back for the flax). bankFlax and bankStrings are the
// bank's counts as it opened, heldStrings the strings about to be banked. The flax is drawn when the run draws it from the
// bank and the bank holds some, no hand-over falls due on the strings the bank will hold afterwards (a hand-over wants the
// pack empty), and this is not the last cycle of a bounded run.
func DrawFlaxWithStrings(s Settings, bankFlax, bankStrings, heldStrings int, now, handoffNext time.Time, cyclesDone, maxCycles int) bool {
	flax := bankFlax
	if flax <= 0 || !DrawsFlaxFromBank(s.FlaxSource, &flax) {
		return false
	}
	after := bankStrings + heldStrings
	if HandoffAfterBanking(s, &after, now, handoffNext) {
		return false
	}
	return maxCycles <= 0 || cyclesDone+1 < maxCycles
}

// Road is the crafter's first day: wool to crafting 10, then the boats to the Seers bank.
func Road() freshstart.Road {
	return freshstart.Road{Destination: Seers().Bank, Radius: freshstart.DestinationRadius, WoolUntil: BowStringLevel}
}

// ArrivedDescription describes the Seers loop.
const ArrivedDescription = "flax to bow strings"

// DecidePhase is the crafter's phase from live state.
func DecidePhase(s freshstart.Snapshot) freshstart.Phase {
	return freshstart.DecidePhase(s, Road())
}

// DescribePhase is the crafter's status line for a phase.
func DescribePhase(p freshstart.Phase) string {
	return freshstart.Describe(p, ArrivedDescription)
}

// DropNames is what a drop pass drops: balls of wool, and past crafting 10 raw wool too.
func DropNames(crafting int) []string {
	if crafting >= BowStringLevel {
		return []string{freshstart.Ball, freshstart.Wool}
	}
	return []string{freshstart.Ball}
}

// XPToBowStrings is the crafting experience still owed before bow strings open.
func XPToBowStrings(craftingXP int) int {
	owed := levels.XPAtLevel(BowStringLevel) - craftingXP
	if owed < 0 {
		return 0
	}
	return owed
}

// SeersAction is the flax loop's next step.
type SeersAction string

// The Seers loop steps.
const (
	SeersBankAll     SeersAction = "bank everything"
	SeersSpin        SeersAction = "spin flax"
	SeersBankStrings SeersAction = "bank bow strings"
	SeersDrawFlax    SeersAction = "draw flax from the bank"
	SeersPick        SeersAction = "pick flax"
	SeersMuleFlax    SeersAction = "fetch flax from the mule"
)

// SeersView is what the Seers loop reads.
type SeersView struct {
	Pack       []freshstart.PackItem
	PackFull   bool
	FlaxSource string
	// FlaxInBank is nil until a bank visit has counted it.
	FlaxInBank *int
	// NotedFlax is the flax the pack holds as bank notes (a mule kit not banked yet).
	NotedFlax int
	// MuleFlaxDue is MuleFlaxDue's answer: the source asks the mule and no back-off is running.
	MuleFlaxDue bool
}

// DecideSeers picks the Seers step: bank anything foreign (the trip's leftovers, and flax notes a
// mule kit left in the pack), spin a full pack of flax, bank bow strings once full or out of flax,
// draw flax from the bank when set to, fetch a kit from the mule when set to (only with no flax in
// the pack, and not while backing off), and otherwise pick.
func DecideSeers(v SeersView) SeersAction {
	flax, strings_, foreign := 0, 0, false
	keep := SeersKeep()
	for _, item := range v.Pack {
		name := strings.TrimSpace(item.Name)
		switch {
		case name == "":
		case strings.EqualFold(name, Flax):
			flax += countOf(item)
		case strings.EqualFold(name, BowString):
			strings_ += countOf(item)
		case isRubJewellery(name):
			// The run staller's Games necklace or Ring of dueling stays through every deposit, so it is
			// never "foreign": live 7 Oct 2026, all four crafters banked "0 items" every second once the
			// mule had handed each a Ring of dueling.
		case !donorplan.InKeep(name, keep):
			foreign = true
		}
	}
	switch {
	case foreign || v.NotedFlax > 0:
		return SeersBankAll
	case flax > 0 && v.PackFull:
		return SeersSpin
	case strings_ > 0 && (v.PackFull || flax == 0):
		return SeersBankStrings
	case DrawsFlaxFromBank(v.FlaxSource, v.FlaxInBank):
		return SeersDrawFlax
	case flax == 0 && v.MuleFlaxDue && AsksMuleForFlax(v.FlaxSource):
		return SeersMuleFlax
	}
	return SeersPick
}

func countOf(item freshstart.PackItem) int {
	if item.Count < 1 {
		return 1
	}
	return item.Count
}

// Move is how to get to where a Seers step works.
type Move string

// The moves.
const (
	MoveHere      Move = "here"
	MoveWalk      Move = "walk"
	MoveClimbUp   Move = "climb up"
	MoveClimbDown Move = "climb down"
)

// RouteToBank: the bank is on the ground floor, so climb down from the wheel.
func RouteToBank(me tile.Tile) Move {
	if me.Level != Seers().Bank.Level {
		return MoveClimbDown
	}
	return MoveHere
}

// RouteToField: down from the wheel, then walk until inside the field's scope.
func RouteToField(me tile.Tile) Move {
	l := Seers()
	if me.Level != l.Field.Level {
		return MoveClimbDown
	}
	if tile.Chebyshev(me, l.Field) > l.FieldScope {
		return MoveWalk
	}
	return MoveHere
}

// RouteToWheel: the wheel is up a ladder the nav graph does not route through, so walk beside the
// ladder and climb.
func RouteToWheel(me tile.Tile) Move {
	l := Seers()
	if me.Level == l.Wheel.Level {
		return MoveHere
	}
	if tile.Chebyshev(me, l.Ladder) > 1 {
		return MoveWalk
	}
	return MoveClimbUp
}

// PickBatch is the picks per field visit before the loop re-decides (FlaxAIO's batch size).
const PickBatch = 30

// Loc is one visible loc, by name.
type Loc struct {
	Name string
	Tile tile.Tile
	Ops  []string
}

// FlaxKey names a flax tile for the skip list.
func FlaxKey(t tile.Tile) string {
	return fmt.Sprintf("flax:%d,%d,%d", t.X, t.Z, t.Level)
}

// ChooseFlax picks the nearest pickable flax inside the field's scope that is not being skipped.
// It returns -1 when there is none.
func ChooseFlax(locs []Loc, me tile.Tile, skipped func(key string) bool) int {
	l := Seers()
	best, bestDist := -1, 0
	for i, loc := range locs {
		if !strings.EqualFold(strings.TrimSpace(loc.Name), Flax) || !freshstart.HasOp(loc.Ops, PickOp) {
			continue
		}
		if loc.Tile.Level != l.Field.Level || tile.Chebyshev(loc.Tile, l.Field) > l.FieldScope {
			continue
		}
		if skipped != nil && skipped(FlaxKey(loc.Tile)) {
			continue
		}
		if d := tile.Chebyshev(loc.Tile, me); best < 0 || d < bestDist {
			best, bestDist = i, d
		}
	}
	return best
}

// PickResult is what one pick attempt came to.
type PickResult string

// The pick results.
const (
	// PickGot: the flax arrived.
	PickGot PickResult = "picked"
	// PickUnreachable: still standing and never picked, so skip the tile for a while.
	PickUnreachable PickResult = "unreachable"
	// PickTaken: gone without a pick; someone else got it.
	PickTaken PickResult = "taken"
)

// AfterPick classifies a pick from whether flax was gained and whether the stalk still stands.
func AfterPick(gained, stillStanding bool) PickResult {
	switch {
	case gained:
		return PickGot
	case stillStanding:
		return PickUnreachable
	}
	return PickTaken
}

// isRubJewellery reports a charged Games necklace or Ring of dueling, the run staller's rub items.
func isRubJewellery(name string) bool {
	n := strings.ToLower(name)
	return strings.HasPrefix(n, "games necklace(") || strings.HasPrefix(n, "ring of dueling(")
}
