// Package boneplan is BoneBurier's own decisions, ported from rsb0t-scripts'
// bonePlan.ts/boneSettings.ts and the state machine embedded in BoneBurier.ts. It is pure: no
// client, actions, fleet or bot imports and no I/O.
//
// Deliberately not ported here, and why:
//   - boneHud.ts, hudPanel.ts, bankFeed.ts, tallyFeed.ts, updateCheck.ts — the Electron wall's
//     on-screen overlay/update-checker/feed publishers; no headless equivalent, the same
//     boundary drawn around every other ported script.
//   - BoneBurier.ts's own "bank" setting (a specific named BANK_LOCATIONS stand, or
//     NEAREST_BANK). This client has no equivalent named-bank catalog — every run banks at
//     whichever booth is nearest to wherever it currently stands, which is also what the TS
//     script's own NEAREST_BANK default does; only the ability to *pin* a different named
//     stand is left out.
//   - Everything driven only by BoneBurier.ts's own settings-page UI (nudgeRadius/nudgeGoal/
//     nudgeIdle stepper button handlers, hudActions) — the plain settings values they'd
//     produce are read directly instead (see Settings/DefaultSettings below).
package boneplan

import (
	"strings"

	"rsb0t-go/internal/scripts/tile"
)

// DefaultNames is the one word that covers every bone kind — Bones, Big bones, Wolf bones,
// Dragon bones and the rest — matched as "the name contains this".
var DefaultNames = []string{"bones"}

// DefaultBankNames are worth a bank slot rather than the prayer xp, when only some kinds are kept.
var DefaultBankNames = []string{"big bones", "dragon bones"}

// BurialOps are the held ops that consume a bone-like item, best first. Ashes scatter rather
// than bury.
var BurialOps = []string{"Bury", "Scatter"}

// DefaultRadius is the tiles from the anchor searched when the radius setting is left alone.
const DefaultRadius = 15

// PickupSkipMs is how long a drop that refused to come up is left where it lies.
const PickupSkipMs = 30_000

// ---- names ----

// ParseNames tidies a raw comma-separated/whitespace list into lowercased, trimmed, deduped
// entries — ported from parseNames.
func ParseNames(raw []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, entry := range raw {
		for _, part := range strings.Split(entry, ",") {
			name := strings.ToLower(strings.TrimSpace(part))
			if name != "" && !seen[name] {
				seen[name] = true
				out = append(out, name)
			}
		}
	}
	return out
}

// IsBoneName reports whether name (a drop or a pack slot) is ours: its name contains one of
// the listed words.
func IsBoneName(name string, names []string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	if n == "" {
		return false
	}
	for _, p := range names {
		if p != "" && strings.Contains(n, p) {
			return true
		}
	}
	return false
}

// BurialOp returns the op that would consume the item ("Bury" or "Scatter"), or "",false when
// it offers none — the guard that keeps a name match from clicking Bury on something that
// merely reads like a bone.
func BurialOp(actions []string) (string, bool) {
	for _, op := range BurialOps {
		for _, a := range actions {
			if strings.EqualFold(a, op) {
				return op, true
			}
		}
	}
	return "", false
}

// ---- what becomes of a bone ----

type DisposalMode string

const (
	DisposalBury  DisposalMode = "bury"
	DisposalBank  DisposalMode = "bank"
	DisposalSplit DisposalMode = "split"
)

type Disposal string

const (
	ToBury Disposal = "bury"
	ToBank Disposal = "bank"
)

// ParseDisposal reads the disposal setting; anything unrecognised buries, the safe reading.
func ParseDisposal(label string) DisposalMode {
	switch strings.ToLower(strings.TrimSpace(label)) {
	case "bank", "bank them":
		return DisposalBank
	case "split", "bank the listed, bury the rest":
		return DisposalSplit
	default:
		return DisposalBury
	}
}

// ParseSource reads the source setting; anything unrecognised works the ground.
type BoneSource string

const (
	SourceGround BoneSource = "ground"
	SourceBank   BoneSource = "bank"
)

func ParseSource(label string) BoneSource {
	if strings.EqualFold(strings.TrimSpace(label), "bank") || strings.EqualFold(strings.TrimSpace(label), "out of the bank") {
		return SourceBank
	}
	return SourceGround
}

// DisposalFor reports what becomes of a held item — "",false when it is not one of ours at
// all, which is what keeps the deposit matcher off the food, the gear and everything else in
// the pack.
func DisposalFor(name string, mode DisposalMode, names, bankNames []string) (Disposal, bool) {
	if !IsBoneName(name, names) {
		return "", false
	}
	switch mode {
	case DisposalBank:
		return ToBank, true
	case DisposalSplit:
		if IsBoneName(name, bankNames) {
			return ToBank, true
		}
		return ToBury, true
	default:
		return ToBury, true
	}
}

// ---- the working area ----

// InRange reports whether t lies inside the patch of ground the run works: a radius of 0
// means the whole loaded scene, but never another floor — a bone one storey up is not worth a
// staircase. anchor == nil (never set yet) always matches.
func InRange(t tile.Tile, anchor *tile.Tile, radius int) bool {
	if anchor == nil {
		return true
	}
	if t.Level != anchor.Level {
		return false
	}
	return radius <= 0 || tile.Chebyshev(t, *anchor) <= radius
}

// ---- stages ----

type Stage string

const (
	StageCollect  Stage = "collect"
	StageBury     Stage = "bury"
	StageBank     Stage = "bank"
	StageReturn   Stage = "return"
	StageWait     Stage = "wait"
	StageWithdraw Stage = "withdraw"
)

// StageState is everything DecideStage needs, read fresh each pass.
type StageState struct {
	// BuryInPack is bones held that are for the ground, and that offer a burial op.
	BuryInPack int
	// BankInPack is bones held that are for the bank.
	BankInPack int
	PackFull   bool
	// GroundBone is true when a bone lies in range and has not been parked as unreachable.
	GroundBone bool
	// BuryWhenFull gathers a full pack before kneeling, rather than burying each as it comes up.
	BuryWhenFull bool
	// Clearing: the pack was filled and is being emptied, and is not finished yet. See
	// StillClearing's own doc comment for why fullness alone cannot drive this.
	Clearing bool
	// Returning: a bank trip is done but the working ground has not been walked back to.
	Returning bool
	// ForceBank makes the bank trip on a pack with room to spare — what a finishing run asks for.
	ForceBank bool
	Source    BoneSource
}

// DecideStage ports decideStage: bury what is held before fetching more, unless the run was
// asked to fill the pack first — and even then, bury once the pack is full or the ground is
// clear, so nothing is carried away. Burying comes before the bank trip because a kneel frees
// the slot the trip was for.
func DecideStage(s StageState) Stage {
	if s.Source == SourceBank {
		if s.BuryInPack > 0 {
			return StageBury
		}
		return StageWithdraw
	}
	if s.BuryInPack > 0 && (s.PackFull || s.Clearing || !s.BuryWhenFull || !s.GroundBone) {
		return StageBury
	}
	if s.BankInPack > 0 && (s.PackFull || s.ForceBank) {
		return StageBank
	}
	if s.Returning {
		return StageReturn
	}
	if s.GroundBone && !s.PackFull {
		return StageCollect
	}
	if s.BuryInPack > 0 {
		return StageBury
	}
	return StageWait
}

// StillClearing reports whether the pack is still being emptied, given whether it was a
// moment ago. A full pack starts it, an empty one ends it, and in between it holds — which is
// the whole point (the first bone buried makes the pack not-full again; a caller that only
// asked "is it full" would go straight back to fetching with a nearly full pack).
func StillClearing(was bool, buryInPack int, packFull, buryWhenFull bool) bool {
	if buryInPack <= 0 {
		return false
	}
	if !buryWhenFull {
		return false
	}
	return was || packFull
}

func DescribeStage(s Stage) string {
	switch s {
	case StageCollect:
		return "picking bones up"
	case StageBury:
		return "burying"
	case StageBank:
		return "banking"
	case StageReturn:
		return "walking back"
	case StageWithdraw:
		return "drawing bones out"
	case StageWait:
		return "waiting for bones"
	default:
		return string(s)
	}
}

// PickupWaitMs is how long to wait for a drop clicked from distance tiles away to land in the
// pack.
func PickupWaitMs(distance int) int {
	if distance < 0 {
		distance = 0
	}
	return 1500 + distance*700
}

// IdleStopMs is idle minutes that end the run, or (0,false) when the setting says to wait forever.
func IdleStopMs(minutes int) (int, bool) {
	if minutes > 0 {
		return minutes * 60_000, true
	}
	return 0, false
}

// ---- the prayer goal ----

// EtaMinSampleMinutes is minutes of burying before the rate is worth an estimate.
const EtaMinSampleMinutes = 1

type GoalProgress struct {
	// Fraction is how far the run has come from where it started towards the goal, 0-1.
	Fraction float64
	// Minutes is minutes left at the rate so far; MinutesKnown is false while there isn't
	// enough of a rate to say.
	Minutes      float64
	MinutesKnown bool
	Reached      bool
}

// Goal computes GoalProgress from this run's own rate — experience gained over minutes spent
// — rather than any assumed rate per bone, matching goalProgress's own reasoning.
func Goal(xpForGoal, xpNow, xpAtStart int, minutes float64) GoalProgress {
	reached := xpNow >= xpForGoal
	span := xpForGoal - xpAtStart
	gained := xpNow - xpAtStart
	fraction := 1.0
	if !reached && span > 0 {
		fraction = float64(gained) / float64(span)
		if fraction < 0 {
			fraction = 0
		} else if fraction > 1 {
			fraction = 1
		}
	}
	if reached {
		return GoalProgress{Fraction: 1, Minutes: 0, MinutesKnown: true, Reached: true}
	}
	if minutes < EtaMinSampleMinutes || gained <= 0 {
		return GoalProgress{Fraction: fraction, MinutesKnown: false, Reached: false}
	}
	return GoalProgress{
		Fraction:     fraction,
		Minutes:      (float64(xpForGoal-xpNow) / float64(gained)) * minutes,
		MinutesKnown: true,
		Reached:      false,
	}
}
