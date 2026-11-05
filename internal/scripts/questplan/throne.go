package questplan

import (
	"fmt"
	"strings"
)

// The Throne of Miscellania chain (docs/throne_chain_2026-11-05.md): what the quest needs behind it, in the order it is run, and the
// stop rules the dangerous parts of it carry. Pure: it reads quest ids and levels and answers what comes next and why not.
//
//	misc (Throne of Miscellania)  <-  hero (Heroes' Quest), viking/fremennik (The Fremennik Trials)
//	hero                          <-  55 quest points, zanaris (Lost City), dragon (Dragon Slayer), merlin (Merlin's Crystal),
//	                                  arrav (Shield of Arrav, either gang)
//	arrav                         <-  a partner of the OTHER gang (the halves are in the two hideouts and one account needs both)

// The chain's quest ids. Fremennik, Merlin, Lost City and Dragon Slayer already have their own; the three below do not have a module yet.
const (
	ArravID  = "arrav"
	HeroID   = "hero"
	ThroneID = "misc"
)

// ThroneStage is one stop of the chain.
type ThroneStage struct {
	ID       string
	Name     string
	Needs    []string // quest ids that must be complete first
	QP       int      // quest points the account must hold before it can start (0: none)
	Pair     bool     // needs a partner account of the other gang
	Built    bool     // a runnable module exists in this tree
	Proven   bool     // it has completed live
	Blocking string   // what is missing, one line (empty when Built and nothing is missing)
}

// ThroneChain is the chain, ordered so that every stage follows the ones it needs. Built is the state of this build (the bot checks it
// against the registered factories, TestThroneChainBuiltMatchesTheFactories), not a hope.
var ThroneChain = []ThroneStage{
	{ID: "zanaris", Name: "Lost City", Built: true, Blocking: "module unproven live (wave 7); Entrana is a bare-handed trip with a teleport out"},
	{ID: MerlinsCrystalID, Name: "Merlin's Crystal", Built: true, Blocking: "walks into the Wilderness for the Excalibur-free path; parked on 'its walks cannot get there' on most accounts"},
	{ID: DragonSlayerID, Name: "Dragon Slayer", Needs: nil, QP: 32, Built: true, Blocking: "held by HeldRungs; never run live; the maze's demon is level 82"},
	{ID: FremennikID, Name: "The Fremennik Trials", Built: true, Blocking: "parked at Sigmund's trial and at the Sigli and Manni kits; Woodcutting 40, Crafting 40 and Fletching 25"},
	{ID: ArravID, Name: "Shield of Arrav", Pair: true, Built: true, Blocking: "module unproven live: needs a partner account (stage5ThroneChainPartner on both) that trades a key, a half shield and a certificate in the museum"},
	{ID: HeroID, Name: "Heroes' Quest", Needs: []string{"zanaris", MerlinsCrystalID, DragonSlayerID, ArravID}, QP: 55, Pair: true,
		Blocking: "no module: Ice gloves (the Ice Queen, level 111, behind a Mining 50 rockslide), the firebird on Entrana, a lava eel, a candlestick with a partner killing Grip"},
	{ID: ThroneID, Name: "Throne of Miscellania", Needs: []string{HeroID, FremennikID},
		Blocking: "no module: marriage, the anthem, the treaty and 75% approval"},
}

// ThroneStageOf is the stage with id.
func ThroneStageOf(id string) (ThroneStage, bool) {
	for _, s := range ThroneChain {
		if s.ID == id {
			return s, true
		}
	}
	return ThroneStage{}, false
}

// ThroneNext is the next stage the chain can run: the first not complete whose needs are all complete, and built. blocked says, for
// every stage it passed over, the one-line reason; a stage whose needs are not met is "needs X".
func ThroneNext(complete map[string]bool, qp int) (next string, blocked map[string]string) {
	blocked = map[string]string{}
	for _, s := range ThroneChain {
		if complete[s.ID] {
			continue
		}
		var missing []string
		for _, n := range s.Needs {
			if !complete[n] {
				missing = append(missing, n)
			}
		}
		switch {
		case len(missing) > 0:
			blocked[s.ID] = "needs " + strings.Join(missing, ", ")
		case s.QP > 0 && qp < s.QP:
			blocked[s.ID] = fmt.Sprintf("needs %d quest points (have %d)", s.QP, qp)
		case !s.Built:
			blocked[s.ID] = "no module yet: " + s.Blocking
		default:
			if next == "" {
				next = s.ID
			}
		}
	}
	return next, blocked
}

// ThroneRunnable is the chain's ids that have a module, in order: what an opted-in account appends to its ladder.
func ThroneRunnable() []string {
	var out []string
	for _, s := range ThroneChain {
		if s.Built {
			out = append(out, s.ID)
		}
	}
	return out
}

// ThroneUnbuilt is the chain's ids that have no module, in order, for the start line that says what is still missing.
func ThroneUnbuilt() []string {
	var out []string
	for _, s := range ThroneChain {
		if !s.Built {
			out = append(out, s.ID)
		}
	}
	return out
}

// MeleeFloor is the lowest of the four melee levels the chain's fights want before they start (the Dragon Slayer maze and the Hero
// fights). The fights are priced one by one inside their modules; this is the gate in front of a module that has never run live.
func MeleeFloor(attack, strength, defence, hitpoints int) int {
	m := attack
	for _, v := range []int{strength, defence, hitpoints} {
		if v < m {
			m = v
		}
	}
	return m
}

// UnholdDragon says whether an account opted into the chain may be let into the held Dragon Slayer: its melee floor reaches min.
// The deaths rule is the module's own (MaxDeaths); this is only the entry gate, and the reason is the log line.
func UnholdDragon(melee, min int) (bool, string) {
	if melee >= min {
		return true, fmt.Sprintf("melee floor %d reaches the chain's %d", melee, min)
	}
	return false, fmt.Sprintf("melee floor %d is under the chain's %d: Dragon Slayer stays held", melee, min)
}

// The Ice Queen (npc 795; content 289 has her spawn in m38_151 at about 2463,9701 and 2465,9697; the 225 dump gives her level 111, attack 95,
// strength 94, defence 95, 104 hitpoints, crush damage, `death_drop,ice_gloves`). The rockslide that shuts her lair wants Mining 50 and a
// pickaxe (herorockslide). She is the only source of the Ice gloves the Hero's Quest firebird feather needs worn.
const (
	IceQueenNPC    = 795
	IceQueenLevel  = 111
	IceQueenHP     = 104
	IceQueenMining = 50
	IceQueenMaxHit = 24 // a planning figure, not read from content (the 289 config carries none): level 94 strength, crush
)

// IceQueenFit is the entry rule of the Ice Queen fight and the stop rule inside it. The fight is priced before the account walks to
// the rockslide, it is never started without the food, and it is stopped on any of the three losses below, with the reason. maxHit is the
// planning figure unless the caller has a better one.
type IceQueenFit struct {
	Mining       int
	Pickaxe      bool
	HP, MaxHP    int
	Food         int // food items held
	FoodHeal     int // hitpoints one food item heals
	Magic        int // Magic level (the fight is a casting fight: her magic defence is 10)
	BestSpellHit int // the largest hit the best castable spell does, 0 when the account can cast nothing useful
	Prayer       int // Prayer level 43 holds Protect from Melee through the fight
	Dead         int // deaths this account has had on this quest
	MaxDeaths    int
}

// Check answers whether the account may start, with the reason when it may not.
func (f IceQueenFit) Check() (bool, string) {
	switch {
	case f.Mining < IceQueenMining:
		return false, fmt.Sprintf("Mining %d is under the %d the rockslide wants", f.Mining, IceQueenMining)
	case !f.Pickaxe:
		return false, "no pickaxe for the rockslide"
	case f.MaxDeaths > 0 && f.Dead >= f.MaxDeaths:
		return false, fmt.Sprintf("%d death(s) on this fight reach the stop rule of %d", f.Dead, f.MaxDeaths)
	case f.BestSpellHit < 1:
		return false, "nothing castable to hurt her with"
	case f.Prayer < 43:
		return false, fmt.Sprintf("Prayer %d cannot hold Protect from Melee (43) against a crush hit up to %d", f.Prayer, IceQueenMaxHit)
	}
	// Casts to kill her: 104 hitpoints at the average of the best spell (half its max), her magic defence is the lowest of her five.
	avg := f.BestSpellHit / 2
	if avg < 1 {
		avg = 1
	}
	casts := (IceQueenHP + avg - 1) / avg
	if f.FoodHeal < 1 {
		return false, "no food that heals"
	}
	// she hits every four ticks; a cast is a few ticks; unprotected she would do about maxhit/2 per hit, protected a fraction. The rule
	// asks for food to cover the unprotected case once for every three casts, rounded up, so a prayer slip does not kill.
	hits := (casts + 2) / 3
	need := hits * (IceQueenMaxHit / 2)
	have := f.HP + f.Food*f.FoodHeal
	if have < need+IceQueenMaxHit {
		return false, fmt.Sprintf("%d casts to kill her need food for about %d damage plus one maximum hit, the pack and hitpoints give %d", casts, need, have)
	}
	return true, fmt.Sprintf("%d casts, %d hitpoints and food to spend", casts, f.HP)
}

// IceQueenStop is the rule inside the fight: leave (and eat or teleport) when hitpoints fall under half or under one maximum hit and a
// half, whichever is higher.
func IceQueenStop(hp, maxHP int) bool {
	floor := maxHP / 2
	if g := IceQueenMaxHit * 3 / 2; g > floor {
		floor = g
	}
	return hp < floor
}
