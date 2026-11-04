package loot

// Thieving: every pickpocket and every stall or chest steal, on ONE card (owner, 2026-09-23:
// "maybe combine pickpocketing to one panel").
//
// WHY THIS IS A KIND OF ITS OWN, AND NOT JUST A SKILL ON THE OLD KINDS
//
// gather.go left Thieving off the gathering list and said how to group it if users asked:
// give the sources a Skill, which is what makes GroupKey collapse every source of that skill onto
// one card. That alone was not enough, and it is worth writing down why, because it is the
// obvious first attempt: GroupKey keeps the KIND in a skill card's key, and a pickpocket was
// KindNPC while a stall steal was KindLoc. A Skill on each would still have given TWO Thieving
// cards, one under the NPC filter and one under no filter at all (the panel had no "loc" entry).
//
// So both are KindThieving now, with Skill Thieving, which puts them on the card "thieving|thieving".
// Everything else about a source is unchanged:
//
//   - Key() is action:name:id and does not mention the kind, so every persisted record keeps the
//     key it was written under. Migrate (gather.go) applies the same rewrite to a record read back
//     from disk, which is the whole migration: nothing is renamed, merged or overwritten, and only
//     the card a record is drawn on moves.
//   - Model says which cache model the panel draws, since the kind no longer does: ModelNPC for a
//     pickpocket target, ModelLoc for a stall or a chest.
//   - Level stays the npc's combat level, so the per-target rows under the card can still colour it.
//
// A Man KILLED is not touched by any of this. It is still KindNPC, action kill, on its own card:
// a kill and a pickpocket are different rows of loot, which is why Action is part of every key.
//
// PER-TARGET DETAIL
//
// One card must not lose which npc or stall was robbed — that was exactly gather.go's objection
// to grouping. Source.PartKey names the row a source contributes to INSIDE its card (kind, action
// and name, so the three Man ids are one row and a Guard is another), and the control panel counts
// the distinct accounts per part as well as per card, so the breakdown's account figures are
// unions rather than guesses.

// KindThieving groups every pickpocket and every stall or chest steal. The card is titled by Skill.
const KindThieving = "thieving"

// SkillThieving is spelled as gamestate.SkillNames spells it.
const SkillThieving = "Thieving"

// thievingShape is the one rewrite both NPC/Loc (for every source recorded from now on) and
// Migrate (for a source read back from a file an older build wrote) apply.
//
// It is deliberately narrow: only a pickpocket from an npc and a steal from a loc, and only when
// the source does not already name a skill, so it is the identity on anything already current and
// on every other kind — a kill, an open, a random event, a gathering or production source.
func thievingShape(s Source) Source {
	if s.Skill != "" {
		return s
	}
	switch {
	case s.Kind == KindNPC && s.Action == ActionPickpocket:
		s.Kind, s.Skill, s.Model = KindThieving, SkillThieving, ModelNPC
	case s.Kind == KindLoc && s.Action == ActionSteal:
		s.Kind, s.Skill, s.Model = KindThieving, SkillThieving, ModelLoc
	}
	return s
}

// EATEN LEFTOVERS. A pickpocket or a steal is measured as the pack's positive change over a short
// window (WaitGained), and a script that eats inside that window turns a Cake into a "2/3 cake":
// the partial form ARRIVES, so it reads as a gain. The live Guard card carried "2/3 cake" x1 and
// "Slice of cake" x3 next to 152,250 coins (2026-09-23), while content's guard pickpocket gives
// coins and nothing else.
//
// No thieving target in content 289 gives a partly eaten food — the pickpocket tables and the
// stall tables are all under content/scripts/skill_thieving, and none of them names one — so a
// take that "gained" one is always the eating, never the steal. The ids are content 289's own
// (content/pack/obj.pack): partial_cake, cake_slice, partial_chocolate_cake, the four half
// pizzas and the three half pies. Chocolate slice (1901) is deliberately NOT here: it is the last
// form of a chocolate cake, but it is also a genuine Baker's stall yield, and a skip filter that
// dropped it would lose real loot.
var eatenLeftovers = map[int]bool{
	1893: true, // 2/3 cake
	1895: true, // Slice of cake
	1899: true, // 2/3 chocolate cake
	2291: true, // 1/2 plain pizza
	2295: true, // 1/2 meat pizza
	2299: true, // 1/2 anchovy pizza
	2303: true, // 1/2 pineapple pizza
	2331: true, // Half a meat pie
	2333: true, // Half a redberry pie
	2335: true, // Half an apple pie
}

// IsEatenLeftover reports whether obj id is a partly eaten food — something a thieving take can
// only ever "gain" because the account ate inside the measurement window. It is the skip filter
// every thieving recorder passes to WaitGained.
func IsEatenLeftover(id int) bool { return eatenLeftovers[id] }
