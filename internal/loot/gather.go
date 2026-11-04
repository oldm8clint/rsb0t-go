package loot

import (
	"strings"

	"rsb0t-go/internal/cachecfg"
)

// Gathering: the skills that take a raw resource out of the world, and the one card each gets
// (2026-09-20, owner: "loot panel to also include mining and woodcutting and any other gathering
// skill i forgot to add, have their rock/tree/etc models combined on a pedestal like the men but
// on a single pedestal and can be titled whatever skill they are from").
//
// WHICH SKILLS THESE ARE, AND WHY
//
// Judged against the game's own list (gamestate.SkillNames, which is the protocol's table and
// the authority — users said he may have missed some, so this was enumerated rather than
// recalled). Content 289 has nineteen live skills: Attack, Defence, Strength, Hitpoints, Ranged,
// Prayer, Magic, Cooking, Woodcutting, Fletching, Fishing, Firemaking, Crafting, Smithing,
// Mining, Herblore, Agility, Thieving, Runecraft.
//
// Three of them produce an item that did not exist before, taken from a world entity:
// FISHING (a fish from a fishing spot), WOODCUTTING (logs from a tree) and MINING (ore from a
// rock). Those are the gathering skills here.
//
// The rest are not, and it is worth saying why rather than leaving it to inference:
//   - Cooking, Fletching, Crafting, Smithing, Herblore, Firemaking and Runecraft all CONSUME an
//     item to make another. They are production; their input came from somewhere else.
//   - Attack, Defence, Strength, Hitpoints, Ranged, Prayer, Magic and Agility produce no item at
//     all. What a kill drops is already recorded, as an npc source.
//   - THIEVING is the genuine judgement call. It does take items from the world, and on later
//     games it is usually filed as gathering. It was left out here, as a card per npc, until the
//     users asked for it grouped (2026-09-23). It is now its own kind with its own card and a
//     per-target breakdown inside it, rather than a gathering skill, so the Gathering filter
//     keeps meaning fishing, woodcutting and mining — see thieving.go.
//   - Farming and Hunter, the other obvious gathering skills, do not exist in this revision.
//
// ONE CARD PER SKILL
//
// A gathering source keeps its own identity — which spot, which tree, which rock — so the
// per-source detail survives. Only GroupKey collapses them, and it collapses on the SKILL rather
// than the name, which is what puts every tree on one Woodcutting card while the panel still has
// the individual rows underneath.

// KindGathering groups every gathering source in the panel. The card is titled by Skill.
const KindGathering = "gathering"

// legacyKindFishing is the Kind fishing sources carried for the few hours between the Loot
// panel first recording a catch and gathering becoming one kind for all three skills. It is
// retired: nothing produces it any more.
//
// It cannot simply be forgotten, though, because the Loot panel's lifetime totals are
// PERSISTED (internal/controlpanel/lootstats.go writes them into stats-state.json and reads
// them back at startup), and the change that retired it migrated nothing. So every catch
// recorded before it kept the dead kind, and kept grouping under it: the owner saw a "Fishing"
// card beside a separate "Fishing spot" card on the live panel, one holding the 254,683 catches
// taken before the change and the other the handful taken after.
const legacyKindFishing = "fishing"

// Migrate brings a Source read back from a persisted file onto the current shape, and is the
// identity for a Source that is already current. Call it on load; calling it on record as well
// is harmless and guards a build mid-rollout.
//
// Two retired shapes are rewritten:
//
//   - the "fishing" kind (legacyKindFishing above), onto the one gathering Fishing card;
//   - a pickpocket that was KindNPC and a stall steal that was KindLoc, both with no skill, onto
//     the one Thieving card (thieving.go). Every pickpocket and steal persisted before
//     2026-09-23 has that shape — 231 pickpocket and 107 steal records in the live file.
//
// It deliberately does NOT change Key(), which is action:name:id — the action, the name and the
// id are all unchanged by either rewrite, so a migrated record keeps the key it was persisted
// under and cannot collide with or overwrite another. Only GroupKey() changes, which is the
// point: the card.
//
// A third rewrite does change the key, and is the one exception: a random event recorded under a
// wrong npc id (the Genie as 411, which is the Swarm; the Mysterious Old Man as 2344, which is no
// npc) is rewritten to the id the reward table has now (CanonicalEventSource), so its key moves
// with it. The persisted record still sits under the old key, which the control panel's restore
// moves and merges (foldRetiredEventKeys); a Source arriving live is simply already right.
func Migrate(s Source) Source {
	if s.Kind == legacyKindFishing {
		s.Kind = KindGathering
		s.Skill = SkillFishing
		s.Action = ActionFish
		s.Model = ModelNPC
	}
	if canon, ok := CanonicalEventSource(s); ok {
		s = canon
	}
	// An oyster opened is on the Fishing card (oysterOpen), whichever shape it was recorded in.
	if (s.Kind == KindOpenable || s.Kind == KindGathering) && s.Action == ActionOpen && IsOysterName(s.Name) {
		s = oysterOpen(s.ID, s.Name)
	}
	return thievingShape(sharedKill(s))
}

// LEGACY GROUND PICKUPS: the one Source shape that cannot be migrated in place.
//
// ScarabSwarm's bone pickups after a cow kill were recorded as an OPENABLE named "Ground" with id
// -1 (scarabrunner/bones.go, 2026-09-22 until 2026-09-23), so the live Cow card showed 97 kills
// and nothing dropped while a separate "Ground" card held their 106 Bones with zero events. The
// right home for those items is the kill that dropped them, which is a DIFFERENT record under a
// different key — so unlike Migrate this needs the caller to move items between records
// (internal/controlpanel's restore does it). IsLegacyGroundPickup names the shape; nothing
// produces it any more.
//
// The only producer was scarabrunner.recordGroundTake, and it only ran at the Ardougne cow field
// straight after a Cow kill (scarabplan.TrainMonster), so every such record is Cow drops.
// LegacyGroundKill is the source they belong to: content 289's cow, npc 81, combat level 2 — the
// only kill source any of the seven affected accounts has.
func IsLegacyGroundPickup(s Source) bool {
	return s.Kind == KindOpenable && s.Action == ActionOpen && s.ID == -1 && strings.EqualFold(strings.TrimSpace(s.Name), "Ground")
}

// LegacyGroundKill is the kill a legacy Ground pickup's items are folded onto when the account
// has no Cow kill record of its own to take them.
var LegacyGroundKill = Source{Kind: KindNPC, Action: ActionKill, Name: "Cow", ID: 81, Level: 2}

// The gathering skills, spelled as gamestate.SkillNames spells them.
const (
	SkillFishing     = "Fishing"
	SkillWoodcutting = "Woodcutting"
	SkillMining      = "Mining"
)

// What one event of each is, for the panel's verb.
const (
	ActionFish = "fish"
	ActionChop = "chop"
	ActionMine = "mine"
)

// FishingSpotName is the name content 289 gives every fishing-spot npc, and so the name every
// spot's loot is recorded under.
const FishingSpotName = "Fishing spot"

// Gathering is one gathering source: one fishing spot, tree or rock.
//
// entity is the npc type id for a fishing spot and the LOC type id for a tree or a rock, which
// is why Model says which of the two the panel should draw. name is resolved through cfg when it
// can be, so the card shows the cache's own wording.
func Gathering(skill, action string, model string, entity int, name string) Source {
	return Source{
		Kind:   KindGathering,
		Skill:  skill,
		Action: action,
		Name:   name,
		ID:     entity,
		Model:  model,
	}
}

// Fishing is a catch source for one fishing-spot npc type.
//
// Every fishing-spot npc in content 289 is named "Fishing spot" whatever method it offers, so
// they would collapse into one card on the name alone; they collapse on the skill now, which is
// the same answer by a more general route.
func Fishing(cfg *cachecfg.Config, typeID int) Source {
	name := FishingSpotName
	if cfg != nil {
		if n, ok := cfg.Npc(typeID); ok && n.Name != "" {
			name = n.Name
		}
	}
	return Gathering(SkillFishing, ActionFish, ModelNPC, typeID, name)
}

// Woodcutting is a source for one tree loc type. Unlike fishing spots, trees have distinct names
// (Tree, Oak, Willow, Maple), so the per-source rows are worth keeping and the skill is what
// brings them onto one card.
func Woodcutting(cfg *cachecfg.Config, locType int, fallback string) Source {
	return Gathering(SkillWoodcutting, ActionChop, ModelLoc, locType, locName(cfg, locType, fallback))
}

// Mining is a source for one rock loc type.
func Mining(cfg *cachecfg.Config, locType int, fallback string) Source {
	return Gathering(SkillMining, ActionMine, ModelLoc, locType, locName(cfg, locType, fallback))
}

func locName(cfg *cachecfg.Config, locType int, fallback string) string {
	if cfg != nil {
		if l, ok := cfg.Loc(locType); ok && l.Name != "" {
			return l.Name
		}
	}
	return fallback
}
