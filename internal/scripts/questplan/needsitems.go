package questplan

import (
	"fmt"
	"strings"
)

// THE ITEM SIDE OF THE REQUIREMENT MODEL (plan 3.2 A and 3.6).
//
// A level a quest wants is a number; an item is a way of getting it, and the way decides whether any training is
// wanted at all. Silver bars come from the mule (187 of them on 19 Oct 2026), or from Mining 20 and Smithing 20 when it
// has none: an account at Mining 1 whose mule can answer needs no Mining, and one whose mule cannot needs a Mining
// trainer that does not exist yet. The model therefore keeps, for each item whose acquisition matters, the CHAIN the
// owner's item rule walks (bank, mule, ground, market, skill, shop), and judges which link an account can use now.
//
// ONLY THE ITEMS WHERE A CHAIN MATTERS ARE HERE: the scarce intermediates of plan 3.6 and the items whose skill link
// carries a level (the iron ore of Doric's Quest, the nails of Horror and Dragon Slayer). A quest's ordinary kit (an
// egg, a pot, a bucket) is the provisioner's and is not a requirement the needs engine plans for. The tool lines (hammer,
// pickaxe, tinderbox, rod) are the provisioner's too (tools.go, lane N4): every tool has a shop, so a tool line always resolves
// and is no chain to plan. The combat-rune supply is lane X5's and is not modelled yet; the report says so.

// Producer is who makes an item the fleet's stock of cannot cover.
type Producer string

const (
	// ProducerNone: the item is bought, found or asked for; nobody makes it.
	ProducerNone Producer = ""
	// ProducerMule: the mule's own stock is the supply.
	ProducerMule Producer = "mule"
	// ProducerDesignated: a designated producer account makes it and publishes it through the mule (plan decisions D4, D13), and an
	// ordinary account keeps the skill link as its fallback when the mule has none (plan 3.6, silver: Mining 20 and Smithing 20, which
	// Elemental Workshop wants of it anyway).
	ProducerDesignated Producer = "designated"
	// ProducerExclusive: ONLY a designated producer account makes it (plan 3.5, "Nails are a producer problem, not a per-account
	// level"; D4, D13). Smithing 34 for 283 accounts is about 810 iron bars each, 229k against the 14k on the mule, so an ordinary
	// account never trains the skill link: it asks the mule and waits for the producer's stock (ItemProducer). The producer
	// (Subject.IsProducer) is the one account that plans the training.
	ProducerExclusive Producer = "exclusive"
)

// ChainLink is one way to get an item, in the item rule's order.
type ChainLink struct {
	Kind SourceKind
	// Skill, Level and Then are a skill link's gates: the level its executor needs, and any further level the product needs
	// (silver ore is mined at Mining 20 and smelted at Smithing 20).
	Skill string
	Level int
	Then  []SkillReq
	// Method names the executor (a Provisioner.Methods entry or a quest's custom step).
	Method string
	// Built is whether that executor exists and works: a link that is not built is a named gap, not a way.
	Built bool
	// Worlds restricts the link to those worlds (the Seers market stands on world 1 only); empty means every world.
	Worlds []int
	Note   string
}

// ItemNeed is one item a quest wants that the needs engine plans for.
type ItemNeed struct {
	Item   int
	Name   string
	Qty    int
	Quests []string
	// WaivedBy and WaivedQty: owning that many of that object makes the item unnecessary (the silver sickle, for the silver
	// bar it would be cast from).
	WaivedBy, WaivedQty int
	// Chain is the ways to get it, in the owner's item rule order (the bank, the account's own, is every chain's first
	// and is implied by Own: it is listed only when a note is worth making).
	Chain []ChainLink
	// Producer says who makes the item (JudgeItem and the planner read it for ProducerExclusive: an ordinary account does not train
	// that item's skill link).
	Producer Producer
	// Scarce marks an item whose stock is thin for the fleet (plan risk 6): a producer must keep up with consumption. It is a
	// label for the reports; no judgement reads it.
	Scarce bool
	// Lane is the plan lane that builds or completes the chain.
	Lane string
	// Gap is non-empty when the chain has no complete acquirer today, and says why.
	Gap    string
	Source string
}

// Need is the NeedItem of quest id.
func (in ItemNeed) Need(id string) Need {
	return Need{Quest: id, Kind: NeedItem, Strength: StrengthHard, ItemID: in.Item, ItemName: in.Name, Qty: in.Qty,
		WaiverItem: in.WaivedBy, WaiverQty: in.WaivedQty, Origins: []Origin{OriginKit}, Note: in.Gap}
}

// ItemState is how far an account is from an item.
type ItemState int

const (
	// ItemHeld: the account's own holdings cover it, or an owned item waives it.
	ItemHeld ItemState = iota
	// ItemMuleHas: the mule holds enough and can be reached.
	ItemMuleHas
	// ItemMuleFar: the mule holds enough and is on another world, and nothing else gets it.
	ItemMuleFar
	// ItemProvisioner: a ground spawn, a market, a shop or a skill source the account can already use gets it; the quest
	// need not wait.
	ItemProvisioner
	// ItemTrain: only a skill link gets it, and the account is below its level.
	ItemTrain
	// ItemStuck: no link is open.
	ItemStuck
	// ItemProducer: only the designated producer makes it (ProducerExclusive) and the mule has too little: the account asks the mule
	// and waits, and trains nothing for it.
	ItemProducer
	// ItemUnjudged: the account's holdings were not read.
	ItemUnjudged
)

func (s ItemState) String() string {
	switch s {
	case ItemHeld:
		return "held"
	case ItemMuleHas:
		return "mule has it"
	case ItemMuleFar:
		return "mule is on another world"
	case ItemProvisioner:
		return "provisioner gets it"
	case ItemTrain:
		return "needs training"
	case ItemStuck:
		return "no source"
	case ItemProducer:
		return "waiting for the designated producer"
	}
	return "unjudged"
}

// ItemJudgement is JudgeItem's verdict.
type ItemJudgement struct {
	State ItemState
	// Link is the chain link the verdict rests on, -1 when none.
	Link int
	// Skills are ItemTrain's skill needs (each ServesItem the item), the levels the account lacks.
	Skills []Need
	Reason string
}

// JudgeItem says which way of getting an item an account can use now: its own holdings first, then each chain link in
// the owner's rule order, skipping a link that is not built, is on another world, or whose stock cannot cover the
// shortfall. A mule that holds the item but cannot be reached is remembered, and is the verdict when nothing else opens.
func JudgeItem(sub Subject, set NeedsSettings, in ItemNeed) ItemJudgement {
	if !sub.OwnKnown {
		return ItemJudgement{State: ItemUnjudged, Link: -1, Reason: "the account's holdings were not read"}
	}
	if in.WaivedBy > 0 && sub.Own[in.WaivedBy] >= max(in.WaivedQty, 1) {
		return ItemJudgement{State: ItemHeld, Link: -1, Reason: fmt.Sprintf("waived: it already holds object %d", in.WaivedBy)}
	}
	have := sub.Own[in.Item]
	if have >= in.Qty {
		return ItemJudgement{State: ItemHeld, Link: -1}
	}
	missing := in.Qty - have
	reach := sub.muleReachable(set.MuleHop)
	var why []string
	far := ""
	producer := false // an exclusive item's skill link was skipped because the account is not the producer
	for i, l := range in.Chain {
		switch l.Kind {
		case SourceBank:
			continue
		case SourceMule:
			stock := sub.MuleStock[in.Item]
			switch {
			case !sub.MuleKnown:
				why = append(why, "the mule's bank is not known")
			case stock < missing:
				why = append(why, fmt.Sprintf("the mule holds %d of the %d wanted", stock, missing))
			case !reach:
				far = fmt.Sprintf("the mule holds %d but is on world %d and the account on world %d", stock, sub.MuleWorld, sub.World)
				why = append(why, far)
			default:
				return ItemJudgement{State: ItemMuleHas, Link: i}
			}
		case SourceGround, SourceMarket, SourceShop:
			switch {
			case !l.Built:
				why = append(why, string(l.Kind)+" source not built")
			case len(l.Worlds) > 0 && sub.World != 0 && !intIn(l.Worlds, sub.World):
				why = append(why, fmt.Sprintf("%s source is on worlds %v only", l.Kind, l.Worlds))
			default:
				return ItemJudgement{State: ItemProvisioner, Link: i}
			}
		case SourceSkill:
			if !l.Built {
				why = append(why, fmt.Sprintf("the %s executor is not built", l.Method))
				continue
			}
			if in.Producer == ProducerExclusive && !sub.IsProducer {
				producer = true
				continue
			}
			var short []Need
			if l.Level > 0 && sub.Level(l.Skill) < l.Level {
				short = append(short, Need{Kind: NeedSkill, Strength: StrengthHard, Skill: l.Skill, Level: l.Level, ServesItem: in.Item,
					Origins: []Origin{OriginKit}, Note: "to make " + in.Name + " (" + l.Method + ")"})
			}
			for _, t := range l.Then {
				if sub.Level(t.Skill) < t.Level {
					short = append(short, Need{Kind: NeedSkill, Strength: StrengthHard, Skill: strings.ToLower(t.Skill), Level: t.Level, ServesItem: in.Item,
						Origins: []Origin{OriginKit}, Note: "to make " + in.Name + " (" + l.Method + ")"})
				}
			}
			if len(short) == 0 {
				return ItemJudgement{State: ItemProvisioner, Link: i}
			}
			return ItemJudgement{State: ItemTrain, Link: i, Skills: short, Reason: l.Method}
		}
	}
	if far != "" {
		return ItemJudgement{State: ItemMuleFar, Link: -1, Reason: far}
	}
	if producer {
		reason := "the designated producer makes " + strings.ToLower(in.Name)
		if len(why) > 0 {
			reason += " (" + strings.Join(why, "; ") + ")"
		}
		return ItemJudgement{State: ItemProducer, Link: -1, Reason: reason}
	}
	reason := strings.Join(why, "; ")
	if in.Gap != "" {
		reason = in.Gap
	}
	if reason == "" {
		reason = "no way to get it is open"
	}
	return ItemJudgement{State: ItemStuck, Link: -1, Reason: reason}
}

func intIn(list []int, v int) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// ItemNeeds is the table. Quantities and ids are the modules' own constants where they have one; the chains and stock
// figures are plan 3.6's (19 Oct 2026) and say so in Source.
var ItemNeeds = []ItemNeed{
	{Item: ObjSilverBar, Name: "Silver bar", Qty: 1, Quests: []string{NatureSpiritID}, WaivedBy: ObjSilverSickle, WaivedQty: 1,
		Chain: []ChainLink{
			{Kind: SourceBank},
			{Kind: SourceMule, Note: "187 bars on 19 Oct 2026; the kit used to ask for ore, which nobody can mine"},
			{Kind: SourceSkill, Skill: "mining", Level: 20, Then: []SkillReq{{Skill: "smithing", Level: 20}}, Method: MethodMineSilver, Built: true,
				Note: "mine-silver works; the smelt step does not make the bar (18 lines, 7 accounts), lane X1"},
		},
		Producer: ProducerDesignated, Scarce: true, Lane: "X1",
		Source: "plan 3.6 Silver bar; naturespirit.go sickleStep"},
	{Item: ObjTrout, Name: "Trout", Qty: deathSupplyQty, Quests: []string{DeathPlateauID},
		Chain: []ChainLink{
			{Kind: SourceBank}, {Kind: SourceMule},
			{Kind: SourceSkill, Skill: "fishing", Level: 20, Method: "fish-trout", Built: false, Note: "fly rod and feathers, then a range"},
		},
		Lane: "X2, T3", Gap: "no trout source: the mule holds none and no fishing executor exists (T3, X2)",
		Source: "plan 3.6 10 cooked trout; deathplateau.go deathSupplyQty"},
	{Item: DoricIronOre, Name: "Iron ore", Qty: DoricIronQty, Quests: []string{DoricsQuestID},
		Chain: []ChainLink{
			{Kind: SourceBank},
			{Kind: SourceMule, Note: "the mule holds iron ore by the thousand, on world 1"},
			{Kind: SourceSkill, Skill: "mining", Level: DoricIronLevel, Method: CustomDoricMine, Built: true, Note: "Rimmington's iron rocks"},
		},
		Producer: ProducerMule, Lane: "N5, T1",
		Source: "doricsquest.go DoricIronQty and DoricIronLevel; plan 3.6 (the mule is on world 1, 32 blocked events)"},
	{Item: ObjNails, Name: "Steel nails", Qty: 8, Quests: []string{HorrorID},
		Chain: []ChainLink{
			{Kind: SourceBank}, {Kind: SourceMule, Note: "the mule holds none"},
			{Kind: SourceSkill, Skill: "smithing", Level: 34, Method: MethodSmithNails, Built: true, Note: "2 nails a steel bar; the mule holds 116 steel bars"},
		},
		Producer: ProducerExclusive, Scarce: true, Lane: "T2, X4",
		Source: "plan 3.5 Nails are a producer problem; horror.go"},
	{Item: ObjNails, Name: "Steel nails", Qty: 12, Quests: []string{DragonSlayerID},
		Chain: []ChainLink{
			{Kind: SourceBank}, {Kind: SourceMule, Note: "the mule holds none"},
			{Kind: SourceSkill, Skill: "smithing", Level: 34, Method: MethodSmithNails, Built: true, Note: "2 nails a steel bar; the mule holds 116 steel bars"},
		},
		Producer: ProducerExclusive, Scarce: true, Lane: "T2, X4",
		Source: "plan 3.5 Nails are a producer problem; dragonslayer.go"},
	{Item: ObjAntiDragon, Name: "Anti-dragon shield", Qty: 1, Quests: []string{ScorpionCatcherID, DragonSlayerID},
		Chain: []ChainLink{{Kind: SourceBank}, {Kind: SourceMule}},
		Lane:  "X4", Gap: "no acquirer: Oziach's hint and the Duke's hand-over are not built (X4)",
		Source: "plan 3.6 Anti-dragon shield"},
	{Item: ObjMithrilSword, Name: "Mithril sword", Qty: 1, Quests: []string{FremennikID},
		Chain: []ChainLink{{Kind: SourceBank}, {Kind: SourceMule}},
		Lane:  "X3", Gap: "no source: the bank and the mule hold none (144 blocked events) and nothing trains Smithing 54 for one (X3)",
		Source: "plan 3.6 Mithril sword"},
	{Item: ObjShark, Name: "Shark", Qty: 10, Quests: []string{FremennikID},
		Chain:    []ChainLink{{Kind: SourceBank}, {Kind: SourceMule, Note: "68.6k on 19 Oct 2026"}},
		Producer: ProducerMule, Lane: "X3",
		Source: "plan 3.6 sharks; fremennik.go"},
	{Item: ObjBones, Name: "Bones", Qty: DemonBonesNeeded, Quests: []string{DemonSlayerID},
		Chain: []ChainLink{
			{Kind: SourceBank}, {Kind: SourceMule},
			{Kind: SourceSkill, Skill: "attack", Method: MethodDemonBones, Built: true, Note: "the Lumbridge cows' bones; no level"},
		},
		Producer: ProducerMule, Lane: "T7",
		Source: "demonslayer.go DemonBonesNeeded; plan 3.6 25 bones"},
	{Item: 3032, Name: "Agility potion(4)", Qty: 1, Quests: []string{TaiBwoID},
		Chain: []ChainLink{
			{Kind: SourceBank}, {Kind: SourceMule, Note: "the mule holds none"},
			{Kind: SourceSkill, Skill: "herblore", Level: 34, Method: "make-agility-potion", Built: false, Note: "toadflax and toad's legs"},
		},
		Lane: "T5, X12a", Gap: "no agility potion source: the mule holds none and Herblore 34 is not trained (T5)",
		Source: "plan 3.6 4 agility potion doses; taibwodecide.go"},
	{Item: ObjPlank, Name: "Plank", Qty: ObsPlanksNeeded, Quests: []string{ObservatoryID},
		Chain: []ChainLink{
			{Kind: SourceBank},
			{Kind: SourceGround, Built: true, Note: "the Barbarian Outpost's four spawns (ObsPlankSpawns)"},
		},
		Lane:   "X12b",
		Source: "observatory.go ObsPlanksNeeded; plan 3.6 Planks, bar, glass"},
}

// ItemChainFor is the item needs of quest id (the account-independent chain report).
func ItemChainFor(id string) []ItemNeed {
	var out []ItemNeed
	for _, in := range ItemNeeds {
		for _, q := range in.Quests {
			if q == id {
				out = append(out, in)
			}
		}
	}
	return out
}
