package questplan

import (
	"fmt"
	"sort"
	"strings"

	"rsb0t-go/internal/scripts/tile"
)

// The provisioner: the owner's item rule (2026-09-23), built once for every quest.
//
//	Every quest ACQUIRES ITS OWN ITEMS: own bank, then check the mule and request everything it
//	holds in ONE bundled ask (never more than 28 unstacked items), then ground spawns, then its
//	own skills/levels (training the level if needed), then buy.
//
// This file is the pure half: given what the account holds, what its bank and the mule's
// PUBLISHED bank hold, its levels and its coins, PlanProvision says which source covers each
// line of a kit. The runner (questrunner/provision.go) executes the plan's first phase and asks
// again, so a withdrawal that came up short, a mule that declined or a shop that was empty all
// fall through to the next rung on the following pass rather than needing their own branches.
//
// Ported in spirit from engine/provisioning.ts planProvisioning, which only knew bank-then-
// gather; the mule rung, the ground/skill/shop rungs and the bundle cap are new.

// SourceKind is one rung of the item rule.
type SourceKind string

const (
	// SourceBank is the account's own bank.
	SourceBank SourceKind = "bank"
	// SourceMule is one bundled ask to the mule, filtered by its published bank.
	SourceMule SourceKind = "mule"
	// SourceGround is a ground spawn.
	SourceGround SourceKind = "ground"
	// SourceMarket is a PLAYER-run market stall (the Seers' Village stalls), paid from the
	// account's own coins like a shop. Method names the runner executor that buys there (a
	// Provisioner.Methods entry, as for a skill source): the stalls, their prices and the fleet's
	// one-buyer-at-a-time turn are the bot's to know, not this package's. Price is the most the
	// line may cost EACH, which is what the coins are budgeted at.
	SourceMarket SourceKind = "market"
	// SourceSkill is the account's own skill: mining, thieving, picking, crafting ...
	SourceSkill SourceKind = "skill"
	// SourceShop is a shop, paid from the account's own coins.
	SourceShop SourceKind = "shop"
)

// ItemRule is the owner's order. PlanProvision never reorders it: a line's own Sources list
// only says WHICH ground spawns, skills and shops exist for it, and they are tried in this
// order whatever order the list gives them in.
//
// The player market sits before the account's own skills (owner, 2026-09-28: the Seers stalls
// "all sell essence that's required for Priest in Peril", while mining it needs Rune Mysteries,
// which the accounts do not have). Only a line that lists a market source is affected.
var ItemRule = []SourceKind{SourceBank, SourceMule, SourceGround, SourceMarket, SourceSkill, SourceShop}

// ruleRank orders the explicit sources by the item rule.
func ruleRank(k SourceKind) int {
	for i, r := range ItemRule {
		if r == k {
			return i
		}
	}
	return len(ItemRule)
}

// MaxBundleSlots is the owner's cap on one mule ask: at most 28 unstacked items, which is one
// trade screen.
const MaxBundleSlots = 28

// CoinsID is the coins object.
const CoinsID = 995

// Source is one explicit way to get an item that is not the bank or the mule (those two are
// implied for every tradeable line).
type Source struct {
	Kind SourceKind

	// Tile is a ground spawn's tile, or where a skill source is worked.
	Tile tile.Tile
	// PerSpawn is how many one pickup of a ground spawn yields.
	PerSpawn int
	// RespawnTicks is how long a ground spawn takes to come back after a pickup.
	RespawnTicks int

	// Skill and Level are a skill source's gate; Method names the runner executor that
	// works it ("thieve-cake", "mine-clay"). A skill source below its level is skipped, not
	// trained: training is the ladder's job (ladder.go), which places a training rung before
	// the quest that needs it.
	Skill  string
	Level  int
	Method string

	// Shop is a shop source's keeper; Price is coins EACH, the content's base cost. It is a
	// floor for budgeting: the real price rises as stock falls.
	Shop  ShopRef
	Price int

	// Note is a free-text reason or caveat for the log and the manifest.
	Note string
}

// Label is the source's log form.
func (s Source) Label() string {
	switch s.Kind {
	case SourceGround:
		return fmt.Sprintf("ground spawn at %d,%d,%d", s.Tile.X, s.Tile.Z, s.Tile.Level)
	case SourceSkill:
		return fmt.Sprintf("%s (%s %d)", s.Method, s.Skill, s.Level)
	case SourceShop:
		return fmt.Sprintf("%s's shop (%d gp each)", s.Shop.Npc, s.Price)
	case SourceMarket:
		return fmt.Sprintf("%s (player market, at most %d gp each)", s.Method, s.Price)
	}
	return string(s.Kind)
}

// KitLine is one item a quest step needs in the pack (or worn).
type KitLine struct {
	ID   int
	Name string
	Qty  int
	// Stackable lines take one pack slot whatever the quantity.
	Stackable bool
	// Untradeable is a quest item: never asked of the mule and never bought.
	Untradeable bool
	// Optional lines are taken when a source has them and never waited for.
	Optional bool
	// Worn lines count a worn copy as held.
	Worn bool
	// OnlyIfShort, when non-zero, is the object id of another kit line this line only serves: the
	// line is wanted while that one is still short and not once it is held. Priest in Peril's
	// pickaxe serves the essence it mines: an essence the bank or the mule supplied needs no pickaxe,
	// and a Lumbridge walk to buy one for it is a detour for nothing.
	OnlyIfShort int
	// Min, when positive, makes Qty a CEILING and Min the least the step can work with: the line is
	// planned for as much of Qty as the pack has room for, and it is satisfied once Min is held and the
	// pack has no room for more. A step that works through a stack (Fletching cuts every log it carries)
	// asks for the largest stack it can use and is content with the part that fits. Asking for exactly
	// 26 logs of an account whose run staller's runes and jewellery ride in the pack left the 3 that did
	// not fit as an endless "the pack filled" (live 15 Oct 2026: the Fletching 25 rung parked on it 16
	// times of 16). Once Min is held a line is only topped up from the bank, never asked of the mule,
	// worked for or bought.
	Min int
	// Sources are the explicit ground, skill and shop sources, tried in the item rule's order.
	Sources []Source
}

// Needs is how many of the line the step still lacks to start: Qty less what is held, or for a
// flexible line (Min) how far below Min it is.
func (l KitLine) Needs(pack, worn map[int]int) int {
	want := l.Qty
	if l.Min > 0 {
		want = l.Min
	}
	return max(want-l.Held(pack, worn), 0)
}

// ValidateKitLine reports why a line is malformed, or "".
func ValidateKitLine(l KitLine) string {
	if l.ID <= 0 {
		return "kit line " + l.Name + " has no object id"
	}
	if l.Qty <= 0 {
		return "kit line " + l.Name + " asks for a non-positive quantity"
	}
	if l.Min < 0 || l.Min > l.Qty {
		return "kit line " + l.Name + " has a minimum outside 0 and its quantity"
	}
	for _, s := range l.Sources {
		switch s.Kind {
		case SourceGround:
			if s.Tile == (tile.Tile{}) {
				return "kit line " + l.Name + " has a ground source with no tile"
			}
		case SourceSkill:
			if s.Method == "" || s.Skill == "" {
				return "kit line " + l.Name + " has a skill source with no method or skill"
			}
		case SourceShop:
			if s.Shop.Npc == "" && s.Shop.NpcID == 0 {
				return "kit line " + l.Name + " has a shop source with no keeper"
			}
		case SourceMarket:
			if s.Method == "" || s.Price <= 0 {
				return "kit line " + l.Name + " has a market source with no method or price ceiling"
			}
		case SourceBank, SourceMule:
			return "kit line " + l.Name + " lists the " + string(s.Kind) + ", which every tradeable line gets anyway"
		default:
			return "kit line " + l.Name + " has an unknown source kind " + string(s.Kind)
		}
	}
	return ""
}

// slots is how many pack slots n of this line take.
func (l KitLine) slots(n int) int {
	if n <= 0 {
		return 0
	}
	if l.Stackable {
		return 1
	}
	return n
}

// ItemCount is an object id and a count.
type ItemCount struct {
	ID    int
	Name  string
	Count int
}

// ProvisionState is everything PlanProvision reads.
type ProvisionState struct {
	// Pack and Worn are the account's own containers by object id.
	Pack, Worn map[int]int
	// Bank is the last complete bank read; BankKnown false means none yet. An unread bank is
	// not an empty one, so an unread bank makes the plan read it before anything else.
	Bank      map[int]int
	BankKnown bool
	// Mule is the mule's PUBLISHED bank, MuleKnown false when there is no mule or it has not
	// been heard. MuleAsked is true once this provisioning round's one bundled ask happened,
	// whatever it yielded: the owner allows ONE ask, and the mule is an accelerator, never a
	// dependency, so a decline falls through rather than being retried.
	Mule      map[int]int
	MuleKnown bool
	MuleAsked bool
	// Levels are base skill levels by lower-case name.
	Levels map[string]int
	// FreeSlots is the pack's free slot count NOW. The runner banks first (deposit
	// everything that is neither kit nor kept), so the plan budgets against the slots that
	// will be free after that deposit: FreeSlots plus the slots Depositable would free.
	FreeSlots int
	// Depositable is how many pack slots the runner's deposit-first would free.
	Depositable int
	// Exhausted names sources that already failed in this provisioning round, by
	// SourceKey(line, source). A failed ground spawn, an empty shop shelf or a skill executor
	// that gave up is skipped for the rest of the round.
	Exhausted map[string]bool
	// Unavailable is why a source in Exhausted was never worked at all: the account cannot use it
	// (the Seers stalls stand on one world, a supply mode that shops at no player market), by
	// SourceKey. The plan skips it like a failed one and says why, not "failed this round".
	Unavailable map[string]string
	// TravelFloat is coins drawn on top of the shop prices (and half again for price rises)
	// whenever a shop task is planned, even when the pack already covers the prices: a
	// shop across the map is reached over ship fares and gates, and the prices are only a floor
	// (content's price rises as the stock falls). Zero draws exactly the prices.
	TravelFloat int
	// CoinsDrawn is set once this provisioning round has already drawn its coins: the margin
	// and the float are then not topped up again, only a pack short of the bare prices goes
	// back to the bank. The fourth Waterfall EMPTY lab run, without it, sailed from Betty back to
	// East Ardougne after every rune line to top up 44-64 coins, four fares a line.
	CoinsDrawn bool
}

// SourceKey identifies one source of one line inside a provisioning round.
func SourceKey(l KitLine, s Source) string {
	return fmt.Sprintf("%d:%s:%s:%d,%d,%d:%s", l.ID, s.Kind, s.Method, s.Tile.X, s.Tile.Z, s.Tile.Level, s.Shop.Npc)
}

// SourceTask is one ground, skill or shop job.
type SourceTask struct {
	Line   KitLine
	Source Source
	// Qty is how many of the line this task should produce.
	Qty int
}

// Describe is the task's log form.
func (t SourceTask) Describe() string {
	return fmt.Sprintf("%d %s from %s", t.Qty, t.Line.Name, t.Source.Label())
}

// ProvisionPhase is what the runner does next.
type ProvisionPhase string

const (
	// PhaseSatisfied means every non-optional line is held.
	PhaseSatisfied ProvisionPhase = "satisfied"
	// PhaseBank means open the bank: read it, deposit what is not kit, withdraw Withdraw.
	PhaseBank ProvisionPhase = "bank"
	// PhaseMule means place the ONE bundled ask, MuleAsk.
	PhaseMule ProvisionPhase = "mule"
	// PhaseSource means run Tasks[0].
	PhaseSource ProvisionPhase = "source"
	// PhaseBlocked means nothing can cover what is short: Blocked says why.
	PhaseBlocked ProvisionPhase = "blocked"
)

// ProvisionPlan is PlanProvision's answer.
type ProvisionPlan struct {
	Phase ProvisionPhase
	// Short is every line's shortfall after the pack (and worn, for Worn lines).
	Short []ItemCount
	// Withdraw is what the own bank covers, plus the coins the shop tasks need.
	Withdraw []ItemCount
	// MuleAsk is the one bundle, never more than MaxBundleSlots unstacked items and never
	// more than the pack can take.
	MuleAsk []ItemCount
	// Tasks are the ground, skill and shop jobs, in kit order.
	Tasks []SourceTask
	// Blocked names the non-optional lines no source covers.
	Blocked []string
	// ShopCoins is what the shop tasks will cost at base price.
	ShopCoins int
}

// Summary is the plan's one-line log form.
func (p ProvisionPlan) Summary() string {
	var parts []string
	if len(p.Withdraw) > 0 {
		parts = append(parts, "bank: "+describeCounts(p.Withdraw))
	}
	if len(p.MuleAsk) > 0 {
		parts = append(parts, "mule: "+describeCounts(p.MuleAsk))
	}
	for _, t := range p.Tasks {
		parts = append(parts, t.Describe())
	}
	if len(p.Blocked) > 0 {
		parts = append(parts, "BLOCKED: "+strings.Join(p.Blocked, "; "))
	}
	if len(parts) == 0 {
		return string(p.Phase)
	}
	return string(p.Phase) + " — " + strings.Join(parts, " | ")
}

func describeCounts(cs []ItemCount) string {
	parts := make([]string, 0, len(cs))
	for _, c := range cs {
		name := c.Name
		if name == "" {
			name = fmt.Sprintf("obj %d", c.ID)
		}
		parts = append(parts, fmt.Sprintf("%d %s", c.Count, name))
	}
	return strings.Join(parts, ", ")
}

// Held is how many of a line the account already has where the step can use it.
func (l KitLine) Held(pack, worn map[int]int) int {
	n := pack[l.ID]
	if l.Worn {
		n += worn[l.ID]
	}
	return n
}

// PlanProvision decides how to fill a kit by the item rule.
//
// The rungs, per line, in order:
//
//  1. Own bank. An unread bank is read first (PhaseBank with nothing to withdraw), because
//     sourcing past a bank nobody has opened is how the upstream bot crossed two kingdoms for
//     a pickaxe it owned.
//  2. The mule, ONE bundled ask for everything it PUBLISHES and the account still lacks,
//     filtered before any walk (AIO-PLAN A7 step 1): a line the published bank does not hold
//     is never asked for, so an empty mule costs nothing at all. Capped at MaxBundleSlots
//     unstacked items and at the pack's room.
//  3. Ground spawns, 4. own skills (only where the level is already met), 5. shops, each
//     skipped once it has failed this round (Exhausted).
//
// Shop coins are budgeted: pack coins first, then bank coins through the same withdrawal.
func PlanProvision(kit []KitLine, st ProvisionState) ProvisionPlan {
	var p ProvisionPlan
	room := st.FreeSlots + st.Depositable
	bankLeft := copyCounts(st.Bank)
	muleLeft := copyCounts(st.Mule)
	muleSlots := 0
	var needBankRead bool

	shortLine := func(id int) bool {
		for _, k := range kit {
			if k.ID == id && k.Needs(st.Pack, st.Worn) > 0 {
				return true
			}
		}
		return false
	}
	for _, l := range kit {
		held := l.Held(st.Pack, st.Worn)
		short := l.Qty - held
		if short <= 0 {
			continue
		}
		if l.OnlyIfShort != 0 && !shortLine(l.OnlyIfShort) {
			continue // the line it serves is held: it is not wanted
		}
		if l.Min > 0 {
			// A flexible line wants only as much as the pack has room for, as long as that is the least
			// it can work with; the room the earlier lines claimed is not counted.
			fit := minInt(short, max(room, 0))
			if held+fit >= l.Min {
				short = fit
				if held >= l.Min {
					// Enough to start: top it up from the bank if the bank is open and holds more, nothing else.
					if st.BankKnown && short > 0 {
						if have := bankLeft[l.ID]; have > 0 {
							take := minInt(short, have)
							p.Short = append(p.Short, ItemCount{ID: l.ID, Name: l.Name, Count: take})
							p.Withdraw = append(p.Withdraw, ItemCount{ID: l.ID, Name: l.Name, Count: take})
							bankLeft[l.ID] -= take
							room -= l.slots(take)
						}
					}
					continue
				}
			}
		}
		p.Short = append(p.Short, ItemCount{ID: l.ID, Name: l.Name, Count: short})

		// 1. Own bank.
		if !st.BankKnown {
			needBankRead = true
		} else if have := bankLeft[l.ID]; have > 0 {
			take := minInt(short, have)
			p.Withdraw = append(p.Withdraw, ItemCount{ID: l.ID, Name: l.Name, Count: take})
			bankLeft[l.ID] -= take
			room -= l.slots(take)
			short -= take
		}
		if short <= 0 {
			continue
		}
		if needBankRead {
			// Nothing past the bank is planned for a line until the bank has been read.
			continue
		}

		// 2. The mule, one bundle.
		if !l.Untradeable && st.MuleKnown && !st.MuleAsked {
			if have := muleLeft[l.ID]; have > 0 {
				take := minInt(short, have)
				if !l.Stackable {
					take = minInt(take, minInt(MaxBundleSlots-muleSlots, room))
				} else if muleSlots >= MaxBundleSlots || room <= 0 {
					take = 0
				}
				if take > 0 {
					p.MuleAsk = append(p.MuleAsk, ItemCount{ID: l.ID, Name: l.Name, Count: take})
					muleLeft[l.ID] -= take
					muleSlots += l.slots(take)
					room -= l.slots(take)
					short -= take
				}
			}
		}
		if short <= 0 {
			continue
		}

		// 3-5. Explicit sources in the item rule's order.
		srcs := append([]Source(nil), l.Sources...)
		sort.SliceStable(srcs, func(i, j int) bool { return ruleRank(srcs[i].Kind) < ruleRank(srcs[j].Kind) })
		placed := false
		for _, s := range srcs {
			if st.Exhausted[SourceKey(l, s)] {
				continue
			}
			switch s.Kind {
			case SourceSkill:
				if s.Level > 0 && st.Levels[strings.ToLower(s.Skill)] < s.Level {
					continue
				}
			case SourceShop:
				if l.Untradeable {
					continue
				}
				p.ShopCoins += s.Price * short
			case SourceMarket:
				// A player stall is only worth the walk with the whole lot's ceiling already in
				// hand or in the bank: the stall draws the item on the strength of the ask, so
				// the buyer refuses to set out short (woodcutrunner/market.go). Too poor for it
				// falls through to the next source rather than blocking the kit on coins.
				if l.Untradeable || !marketAffordable(s, short, st, bankLeft, p.ShopCoins) {
					continue
				}
				p.ShopCoins += s.Price * short
			}
			p.Tasks = append(p.Tasks, SourceTask{Line: l, Source: s, Qty: short})
			room -= l.slots(short) // the pack space this line will need is spoken for
			placed = true
			break
		}
		if !placed && !l.Optional {
			p.Blocked = append(p.Blocked, fmt.Sprintf("%d %s: %s", short, l.Name, noSourceWhy(l, st)))
		}
	}

	if needBankRead {
		p.Phase = PhaseBank
		p.Withdraw = nil
		p.MuleAsk = nil
		p.Tasks = nil
		p.Blocked = nil
		return p
	}

	// Coins for the shops: pack first, then the bank through the same visit.
	//
	// The pack should leave for the shops holding the prices, half again for content's price
	// rise as a shelf empties, and the travel float for the fares there AND back. Only the bare
	// prices are required: the margin and the float are drawn when the bank has them. The second
	// Waterfall lab run left with the prices covered but no float drawn (the old check only
	// topped up a pack short of the bare prices), spent its coins on Betty's runes and three
	// ship fares, and was left on Musa Point unable to pay the 30 gp ship back.
	if p.ShopCoins > 0 {
		inPack := st.Pack[CoinsID]
		target := p.ShopCoins + p.ShopCoins/2 + st.TravelFloat
		if st.CoinsDrawn {
			target = p.ShopCoins
		}
		if inPack < target {
			want := target - inPack
			have := bankLeft[CoinsID]
			if have > 0 {
				p.Withdraw = append(p.Withdraw, ItemCount{ID: CoinsID, Name: "Coins", Count: minInt(want, have)})
			}
			if short := p.ShopCoins - inPack - have; short > 0 {
				p.Blocked = append(p.Blocked, fmt.Sprintf("the shops need %d gp and the pack and bank are %d short", p.ShopCoins, short))
			}
		}
	}

	switch {
	case len(p.Short) == 0:
		p.Phase = PhaseSatisfied
	case len(p.Withdraw) > 0:
		p.Phase = PhaseBank
	case len(p.MuleAsk) > 0:
		p.Phase = PhaseMule
	case len(p.Tasks) > 0 && !coinsBlocked(p):
		p.Phase = PhaseSource
	case allOptional(kit, p.Short) && len(p.Blocked) == 0:
		p.Phase = PhaseSatisfied
	default:
		p.Phase = PhaseBlocked
	}
	return p
}

// coinsBlocked is true when the only thing between the plan and its shop tasks is money.
func coinsBlocked(p ProvisionPlan) bool {
	for _, b := range p.Blocked {
		if strings.HasPrefix(b, "the shops need") {
			for _, t := range p.Tasks {
				if t.Source.Kind != SourceShop && t.Source.Kind != SourceMarket {
					return false
				}
			}
			return true
		}
	}
	return false
}

// allOptional reports whether every short line is optional.
func allOptional(kit []KitLine, short []ItemCount) bool {
	opt := map[int]bool{}
	for _, l := range kit {
		opt[l.ID] = l.Optional
	}
	for _, s := range short {
		if !opt[s.ID] {
			return false
		}
	}
	return true
}

func noSourceWhy(l KitLine, st ProvisionState) string {
	var why []string
	if l.Untradeable {
		why = append(why, "a quest item (no mule, no shop)")
	} else if !st.MuleKnown {
		why = append(why, "no mule heard")
	} else if st.MuleAsked {
		why = append(why, "the one mule ask is spent")
	} else {
		why = append(why, "the mule publishes none")
	}
	for _, s := range l.Sources {
		switch {
		case st.Unavailable[SourceKey(l, s)] != "":
			why = append(why, s.Label()+": "+st.Unavailable[SourceKey(l, s)])
		case st.Exhausted[SourceKey(l, s)]:
			why = append(why, s.Label()+" failed this round")
		case s.Kind == SourceMarket && !l.Untradeable:
			why = append(why, s.Label()+": the pack and bank cannot pay its ceiling")
		case s.Kind == SourceSkill && st.Levels[strings.ToLower(s.Skill)] < s.Level:
			why = append(why, fmt.Sprintf("%s needs %s %d (have %d)", s.Method, s.Skill, s.Level, st.Levels[strings.ToLower(s.Skill)]))
		}
	}
	if len(l.Sources) == 0 {
		why = append(why, "no ground, skill or shop source listed")
	}
	return strings.Join(why, "; ")
}

// marketAffordable reports whether the pack and bank coins cover a market source's ceiling for
// short units on top of the coins the plan has already committed to other sources.
func marketAffordable(s Source, short int, st ProvisionState, bankLeft map[int]int, committed int) bool {
	return st.Pack[CoinsID]+bankLeft[CoinsID]-committed >= s.Price*short
}

func copyCounts(m map[int]int) map[int]int {
	out := make(map[int]int, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// KitIDs is every object id a kit names, for a deposit-first keep list.
func KitIDs(kit []KitLine) []int {
	out := make([]int, 0, len(kit))
	for _, l := range kit {
		out = append(out, l.ID)
	}
	return out
}

// KitSatisfied reports whether the pack (and worn) already hold every non-optional line.
func KitSatisfied(kit []KitLine, pack, worn map[int]int) bool {
	for _, l := range kit {
		if !l.Optional && l.Held(pack, worn) < l.Qty {
			return false
		}
	}
	return true
}

// KitSlots is how many pack slots a kit takes when full (worn lines excluded).
func KitSlots(kit []KitLine) int {
	n := 0
	for _, l := range kit {
		if l.Worn {
			continue
		}
		n += l.slots(l.Qty)
	}
	return n
}

// ShopItemStep is the one way a module gets a shop item: a provision step of its own (the
// item rule: bank, the mule, then the shop), and a wait once that failed. Never a bare buy: a
// bare buy lets a quest pass with its provisioner disabled, which a lab CONTROL arm must not.
func ShopItemStep(s Snapshot, kit string, id int, name string, qty int, shop ShopRef, price int, keep []int) Step {
	if s.Failed["provision:"+kit] {
		return Step{Kind: StepWait, Reason: "no " + name + " from the bank, the mule or " + shop.Npc}
	}
	bank := NearestBank(s.Tile).Tile
	return Step{Kind: StepProvision, KitName: kit, KeepIDs: keep, Bank: bank, BankSet: true,
		Kit:    []KitLine{{ID: id, Name: name, Qty: qty, Sources: []Source{{Kind: SourceShop, Shop: shop, Price: price}}}},
		Reason: name + " from " + shop.Npc + "'s shop by the item rule"}
}
