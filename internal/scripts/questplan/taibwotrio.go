package questplan

import (
	"fmt"
	"strings"

	"rsb0t-go/internal/scripts/tile"
)

// Tai Bwo Wannai Trio (content id `tbwt`, six varps, two of them transmitted), ported from the TS def (defs/tbwt/*.ts) and checked
// against content 289: quests/quest_tbwt/{configs,scripts} (quest_tbwt.rs2, tbwt_journal.rs2, tbwt_lubufu.rs2, tbwt_tiadeche.rs2,
// tbwt_tamayu.rs2, tbwt_tinsay.rs2, tbwt_jogre_bones.rs2, monkey.rs2), areas/area_karamja/scripts/{tbwt_timfraku,trufitus,zambo,
// customs_officer}.rs2, skill_fishing/scripts/fishing_spots/tbwt.rs2, skill_cooking, skill_magic/scripts/spells/superheat.rs2,
// skill_combat/scripts/weapon_poison.rs2, drop tables/scripts/jogre.rs2 and the maps (m43_46, m43_48, m43_49, m44_47, m45_48, m39_71).
// Two quest points; no experience of its own beyond what the steps pay (Lubufu's lesson pays 1,500 Fishing experience).
//
// WHAT THE OWNER'S BRIEF GOT WRONG. The brief reads "the Shaikahan fight: price it or park". There is no fight. Tamayu's hunt is a
// CUTSCENE: tbwt_tamayu.rs2 [label,tbwt_tamayu_cutscene] teleports the account into a sealed copy of the map (m39_71, x 2496 to 2559,
// z 4544 to 4607), points the camera, and the account watches Tamayu spear a beast (tbwt_beast_cutscene, defaultmode none,
// moverestrict nomove, no ops) while two NPCs play animations; when the account has given Tamayu four doses of agility potion and a
// Karambwan-poisoned spear that is not bronze, the second hunt is the killing one, [tbwt_tamayu_final_cutscene] sets %tbwt_tamayu to
// complete and teleports the account back. The account never lifts a hand. The level 83 Shaikahan that IS a fightable npc (tbwt_beast,
// npc 1172, attack 80, strength 25, 100 hitpoints, aggressive_melee with hunt range 8, wander range 32, spawn m45_48 "0 26 23" =
// 2906,3095) is a roaming hazard of the east of the island, not a step: Tiadeche stands inside its wander box, so the conversations
// there are priced against it (ShaikahanOdds) and the walks cannot be fenced from it.
//
// The story, as the stages each step moves (the journal is the only reading of most of them: %tbwt_main and %tbwt_tiadeche are
// transmit=yes, %tbwt_tinsay, %tbwt_tamayu, %tbwt_lubufu and %tbwt_flags are perm only):
//
//	Timfraku (the chief, upstairs in the hut at Tai Bwo Wannai, 2780,3087 level 1): "Trufitus sent me." (Jungle Potion complete) and
//	    "Yes" to the job: %tbwt_main 3. The conversation has a four-tick pause in the middle ("Timfraku appears to concentrate").
//	Lubufu (the Karambwan fisherman south of Brimhaven, 2770,3169): his first answer to a stranger is a ten-tick brush-off queue that
//	    re-arms on every talk inside it (tbwt_lubufu_setqueue); the second talk, after it, offers the way in ("Talk about him...",
//	    "How old are you?", "I could help collect the bait.", "You sound like you could do with the help."), which asks for 20 raw
//	    Karambwanji (untradeable; caught with a small net in the Holy Lake south of the village, 2791,3019 / 2801,3010 / 2807,3021, and
//	    shrimps come with them); the NPC counts them in himself (a part-load is progress), three questions later he offers the
//	    apprenticeship and hands over a Karambwan vessel (1,500 Fishing experience). A vessel lost can be had again (stage 31 only: the
//	    menu "Actually, I've lost my Karambwan vessel." appears when the pack holds none).
//	Tiadeche (the fisherman, 2912,3118): "When will you be finished?" ends the first talk (%tbwt_tiadeche 2); a vessel loaded with a raw
//	    Karambwanji (the vessel used on the Karambwanji) used on him is the catch: he keeps the vessel and offers his first catch, a raw
//	    Karambwan ("Yes"), and the talk ends in the request (4) to take a vessel to Tinsay. The raw Karambwan is the ONLY free one:
//	    Karambwan fishing is Fishing 65, and a burnt one cannot be had again (below).
//	Tamayu (the hunter, 2844,3042): the first talk sets his vow (2); "When will you succeed?" then "Yes" is a non-killing hunt (3); then
//	    the agility potion (4 doses, any mix, each bottle used on him, only at stage 3), a spear above bronze that is
//	    Karambwan-poisoned (a poorly cooked Karambwan ground with a pestle into the poisonous paste, the spear used on the paste) used on
//	    him, and "Take me on your next hunt" is the killing hunt (4). Each spear he is handed resets the three spear bits from that one
//	    spear, so the one spear must carry both.
//	Tinsay (the priest, 2764,2976): the first talk (2); a banana, sliced with a knife, dropped into a bottle of Karamjan rum (Zambo's
//	    bar sells it, untradeable, and the customs officer confiscates it at the Musa Point ship), used on him (3, and in the same talk 4);
//	    a seaweed sandwiched in a monkey skin (a monkey killed while the quest is running drops a corpse, only Tamayu skins it, and only
//	    once his own hunt is over; the monkey dodges every melee blow and every arrow, but a spell is not an attack option it can
//	    dodge, monkey.rs2 [opnpc2,monkey]) used on him (5, and in the same talk 6); and the bones of a Jogre (always dropped by the
//	    kill) burnt, smothered in a raw Karambwanji ground with a pestle, and cooked on a fire (a marinade that cannot burn: successchance
//	    1,1) used on him (7). Then a vessel (a second one: Lubufu gives another to anyone who carries none) used on him, once his three
//	    deliveries are done, is his crafting manual, which goes to Tiadeche (6), who also sets %tbwt_main to 4 when the other three are
//	    complete. Timfraku pays 2,000 coins and the quest ends (6).
//
// What the content does that a script must plan for:
//   - FIREMAKING IS 1 ON THE WHOLE FLEET (the panel, 19 Oct 2026: highest 3). The TS def burns the Jogre bones with a tinderbox
//     (light_jogre_bones_inv wants Firemaking 30), which no account here can; the only furnace on Karamja is behind Shilo Village.
//     Superheat Item (Magic 43, four fire runes and a nature rune, superheat.rs2 [label,tbwt_jogre_bones_superheat]) burns them
//     anywhere, and the fleet's Magic median is 64. The module therefore wants Magic 43 and a nature rune, and builds no tinderbox
//     route.
//   - THE AGILITY POTIONS AND THE SPEAR ARE NOT IN THE FLEET. Agility potions are Herblore 34 (the panel: one account); not one of
//     358 banks nor the mule holds a bottle (19 Oct 2026). A spear above bronze: 27 banks hold a mithril one and the mule none;
//     Jogres drop an iron one 4 times in 129. The Decide asks the bank and the mule for both before the account leaves the mainland
//     and parks it (six hours, with the reason) when either is missing; the quest cannot finish until the mule is stocked.
//   - Karamja has no bank. Everything is drawn first (coins for the ships and the rum; the tools; the runes; the food), and the account
//     never carries rum off the island.
//   - The raw Karambwan burns: poorly cooked is successchance 70,255 (about one in two at Cooking 30, three in four at 70). A burnt
//     one cannot be had again for nothing; Tiadeche's second gift needs the bit %tbwt_flags 2 clear and the pack without a raw one,
//     which a first "Yes" sets. The module asks him once more (a talk), then fishes one with the vessel at Fishing 65, and parks
//     otherwise.
//   - Every conversation here has gaps longer than one page turn (if_close; p_delay), and chat boxes (mesbox, objbox) that must be
//     continued, never closed: closing one aborts the script that raised it. No food can be eaten inside a conversation, so the
//     Shaikahan is priced by the hitpoints at the start.
//   - The island's walks keep out of the rectangles round every spider, tribesman, guard dog and Jogre spawn of the legs
//     (TbwtAvoid); a route to every stand exists round them on the real pack.

// TaiBwoName is the quest tab's spelling (component 1740, `questlist:tbwt`).
const TaiBwoName = "Tai Bwo Wannai Trio"

// TaiBwoID is the engine's id.
const TaiBwoID = "tbwt"

// The stages the journal can tell apart, one set per brother. TbwtState packs them into the int Module.ReadStage returns.
const (
	// TbPhasePre is %tbwt_main 1 or 2: Timfraku's job is still to be taken. (0 is a red tab, which the engine reads before any stage.)
	TbPhasePre = 1
	// TbPhaseStarted is %tbwt_main 3: the brothers.
	TbPhaseStarted = 2
	// TbPhaseBrothers is %tbwt_main 4: all four are done and Timfraku is to be told.
	TbPhaseBrothers = 3
	// TbPhaseComplete is a page that says the quest is over (the tab is green by then and the engine does not ask).
	TbPhaseComplete = 4
)

// Lubufu's stages as the journal tells them (%tbwt_lubufu, tbwt_journal.rs2).
const (
	// TbLubufuNone is stage 0 and 1: no section.
	TbLubufuNone = iota
	// TbLubufuMet is 2 and 3: "a crochety old fisherman" and nothing more.
	TbLubufuMet
	// TbLubufuOffered is 4: the offer to help is made and not yet accepted.
	TbLubufuOffered
	// TbLubufuBait is 5 to 24: "I need to give Lubufu 20 Karambwanji" (how many are in is not on the page).
	TbLubufuBait
	// TbLubufuGiven is 25 to 28: "I have given Lubufu 20 Karambwanji" (the questions are counted and not on the page either).
	TbLubufuGiven
	// TbLubufuApprentice is 29: he has offered the apprenticeship.
	TbLubufuApprentice
	// TbLubufuDone is 30 and 31: accepted, and the vessel handed over.
	TbLubufuDone
)

// Tiadeche's stages (%tbwt_tiadeche; the journal and the transmitted varp agree on all but 5 and 6).
const (
	TbTiadecheNone = iota
	// TbTiadecheIntro is 1: "Nothing of interest."
	TbTiadecheIntro
	// TbTiadecheWaiting is 2: he returns only once he has caught a Karambwan.
	TbTiadecheWaiting
	// TbTiadecheCaught is 3: caught, and the request not yet made (a stage the next sentence of the same talk moves on).
	TbTiadecheCaught
	// TbTiadecheManual is 4: a vessel is to go to Tinsay for the crafting instructions.
	TbTiadecheManual
	// TbTiadecheDone is 5 and above: the vessel has been to Tinsay (5, the manual is in the pack or lost) or the manual is handed over (6, 7).
	TbTiadecheDone
)

// Tinsay's stages are the content's own (%tbwt_tinsay), and the journal tells every one of them.
const (
	TbTinsayNone = iota
	TbTinsayIntro
	TbTinsayRum
	TbTinsayGivenRum
	TbTinsaySandwich
	TbTinsayGivenSandwich
	TbTinsayBones
	TbTinsayComplete
)

// Tamayu's stages are the content's own (%tbwt_tamayu).
const (
	TbTamayuNone = iota
	TbTamayuIntro
	// TbTamayuSlay is 2: the vow is made, the first hunt not yet watched.
	TbTamayuSlay
	// TbTamayuWatched is 3: one hunt watched. The potion and the spear are handed over at this stage and no other.
	TbTamayuWatched
	TbTamayuComplete
)

// TbwtState is the journal's reading of the quest, and what Snapshot.Stage packs.
type TbwtState struct {
	Phase    int
	Lubufu   int
	Tiadeche int
	Tinsay   int
	Tamayu   int
	// Agility says Tamayu has drunk four doses ("I have increased his agility"); only the fourth dose writes a line, so a part-poured
	// Tamayu reads false. Spear says he holds a spear that is both above bronze and Karambwan-poisoned.
	Agility, Spear bool
}

// Stage packs the state into one int (Module.ReadStage's return): phase 3 bits, Lubufu 3, Tiadeche 3, Tinsay 3, Tamayu 3, two flags.
func (t TbwtState) Stage() int {
	n := t.Phase | t.Lubufu<<3 | t.Tiadeche<<6 | t.Tinsay<<9 | t.Tamayu<<12
	if t.Agility {
		n |= 1 << 15
	}
	if t.Spear {
		n |= 1 << 16
	}
	return n
}

// TbwtStateOf unpacks Stage.
func TbwtStateOf(n int) TbwtState {
	return TbwtState{Phase: n & 7, Lubufu: n >> 3 & 7, Tiadeche: n >> 6 & 7, Tinsay: n >> 9 & 7, Tamayu: n >> 12 & 7,
		Agility: n>>15&1 == 1, Spear: n>>16&1 == 1}
}

// String is the state in a log line.
func (t TbwtState) String() string {
	return fmt.Sprintf("phase %d lubufu %d tiadeche %d tinsay %d tamayu %d agility %v spear %v", t.Phase, t.Lubufu, t.Tiadeche, t.Tinsay, t.Tamayu, t.Agility, t.Spear)
}

// tbwtNormalise is NormaliseLines with the content's own line break ('|') read as a space: a sentence the journal breaks over two lines
// ("marinated in|Karambwanji paste") is one phrase.
func tbwtNormalise(lines []string) string {
	flat := make([]string, len(lines))
	for i, l := range lines {
		flat[i] = strings.ReplaceAll(l, "|", " ")
	}
	return NormaliseLines(flat)
}

// tbwtLabels are the four brothers' headings on the in-progress page, in the order the page prints them.
var tbwtLabels = []string{"tiadeche", "tinsay", "tamayu", "lubufu"}

// tbwtSection is the slice of the page between one heading and the next. "Nothing of interest." opens three of the sections, so a marker
// only counts inside its own.
func tbwtSection(text, label string) string {
	start := strings.Index(text, label+":")
	if start < 0 {
		return ""
	}
	end := len(text)
	for _, other := range tbwtLabels {
		if other == label {
			continue
		}
		if at := strings.Index(text[start+len(label)+1:], other+":"); at >= 0 && start+len(label)+1+at < end {
			end = start + len(label) + 1 + at
		}
	}
	return text[start:end]
}

// tbwtFirst is the first needle the text holds, in the order given (newest stage first: every later page prints the lines of the
// ones before it).
func tbwtFirst(text string, needles []tbwtNeedle) (int, bool) {
	for _, n := range needles {
		if strings.Contains(text, n.needle) {
			return n.stage, true
		}
	}
	return 0, false
}

type tbwtNeedle struct {
	needle string
	stage  int
}

var (
	tbwtLubufuNeedles = []tbwtNeedle{
		{"i have accepted lubufu's offer", TbLubufuDone},
		{"lubufu has offered to train me as his apprentice", TbLubufuApprentice},
		{"i have given lubufu 20 karambwanji", TbLubufuGiven},
		{"i need to give lubufu 20 karambwanji", TbLubufuBait},
		{"lubufu has accepted", TbLubufuBait},
		{"i have offered to help collect bait", TbLubufuOffered},
		{"lubufu is a crochety old fisherman", TbLubufuMet},
	}
	tbwtTiadecheNeedles = []tbwtNeedle{
		{"i have taken a karambwan vessel to tinsay and have retrieved crafting instructions", TbTiadecheDone},
		{"i must take a karambwan vessel to tinsay", TbTiadecheManual},
		{"he has successfully caught a karambwan", TbTiadecheCaught},
		{"once he has caught a karambwan", TbTiadecheWaiting},
		{"nothing of interest", TbTiadecheIntro},
	}
	tbwtTinsayNeedles = []tbwtNeedle{
		{"marinated in karambwanji paste", TbTinsayComplete},
		{"he requires burnt jogre bones", TbTinsayBones},
		{"i have given him a seaweed in monkey skin sandwich", TbTinsayGivenSandwich},
		{"he requires a seaweed in monkey skin sandwich", TbTinsaySandwich},
		{"i have given him sliced banana in karamja rum", TbTinsayGivenRum},
		{"he requires banana in karamja rum", TbTinsayRum},
		{"nothing of interest", TbTinsayIntro},
	}
	tbwtTamayuNeedles = []tbwtNeedle{
		{"tamayu has slain the shaikahan", TbTamayuComplete},
		{"he appears to be having difficulty in the hunt", TbTamayuWatched},
		{"once he has slain", TbTamayuSlay},
		{"nothing of interest", TbTamayuIntro},
	}
)

// ParseTaiBwoJournal reads the quest's state from the rendered journal (tbwt_journal.rs2) and returns it packed (TbwtState.Stage); ok is
// false for an empty or unrecognised page, which is unknown and never stage 0.
//
// Lubufu's section is the last on the page and the page holds fifty lines at most, so a long page may lose it: a Tiadeche who has
// asked for a vessel to go to Tinsay (4 and above) can only have had one from Lubufu, so Lubufu is done whatever the page shows of him.
func ParseTaiBwoJournal(lines []string) (int, bool) {
	text := tbwtNormalise(lines)
	var st TbwtState
	switch {
	case text == "":
		return 0, false
	case strings.Contains(text, "quest complete"):
		return TbwtState{Phase: TbPhaseComplete}.Stage(), true
	case strings.Contains(text, "i have convinced them to return to the village"):
		st.Phase = TbPhaseBrothers
	case strings.Contains(text, "sons of timfraku"):
		st.Phase = TbPhaseStarted
	case strings.Contains(text, "to complete this quest i need"), strings.Contains(text, "i can start this quest by speaking to"):
		return TbwtState{Phase: TbPhasePre}.Stage(), true
	default:
		return 0, false
	}
	st.Tiadeche, _ = tbwtFirst(tbwtSection(text, "tiadeche"), tbwtTiadecheNeedles)
	st.Tinsay, _ = tbwtFirst(tbwtSection(text, "tinsay"), tbwtTinsayNeedles)
	tamayu := tbwtSection(text, "tamayu")
	st.Tamayu, _ = tbwtFirst(tamayu, tbwtTamayuNeedles)
	st.Agility = strings.Contains(tamayu, "i have increased his agility")
	st.Spear = strings.Contains(tamayu, "karambwan poisoned spear")
	// Past the kill both are facts, and the page says so in the same lines.
	if st.Tamayu >= TbTamayuComplete {
		st.Agility, st.Spear = true, true
	}
	lubufu := tbwtSection(text, "lubufu")
	st.Lubufu, _ = tbwtFirst(lubufu, tbwtLubufuNeedles)
	if st.Lubufu >= TbLubufuDone || strings.Contains(text, "i have betrayed the secret of the karambwan to tiadeche") {
		st.Lubufu = TbLubufuDone
	}
	if st.Tiadeche >= TbTiadecheManual {
		st.Lubufu = TbLubufuDone
	}
	return st.Stage(), true
}

// Ids from content/pack (checked against the production config by TestTbwtIdsMatchTheProductionConfig).
const (
	TbNpcTimfraku  = 1162 // [tbwt_timfraku]
	TbNpcTiadeche  = 1163 // [tbwt_tiadeche]
	TbNpcTinsay    = 1165 // [tbwt_tinsay]
	TbNpcTamayu    = 1167 // [tbwt_tamayu]
	TbNpcLubufu    = 1171 // [tbwt_lubufu]
	TbNpcShaikahan = 1172 // [tbwt_beast], the fightable one
	TbNpcMonkey    = 132  // [monkey], level 3, six hitpoints
	TbNpcJiminua   = 560  // [jiminua], Jiminua's Jungle Store

	// The fishing spots: the Holy Lake's Karambwanji shoal (op 1 Net) and the three Karambwan spots (op 1 Fish with a loaded vessel).
	TbNpcKarambwanjiSpot = 1174
	TbNpcKarambwanSpotA  = 1176
	TbNpcKarambwanSpotB  = 1177
	TbNpcKarambwanSpotC  = 1178

	TbLocFire   = 2732 // the permanent jungle fire ("Fire", 2789,3048 and 2790,3048), a cooking fire
	TbLocLadder = 1747 // the ladder up to Timfraku's floor, 2782,3087

	TbObjNet               = 303  // [net], "Small fishing net"
	TbObjPestle            = 233  // [pestle_and_mortar]
	TbObjNatureRune        = 561  // [naturerune]
	TbObjSlicedBanana      = 3162 // [tbwt_sliced_banana]
	TbObjRumSlicedBanana   = 3164 // [tbwt_sliced_banana_in_karamja_rum]
	TbObjVessel            = 3157 // [tbwt_karambwan_vessel]
	TbObjVesselLoaded      = 3159 // [tbwt_karambwan_vessel_loaded_with_karambwanji]
	TbObjManual            = 3161 // [tbwt_crafting_manual]
	TbObjRawKarambwanji    = 3150 // [tbwt_raw_karambwanji], untradeable
	TbObjKarambwanjiPaste  = 3155 // [tbwt_raw_karambwanji_paste]
	TbObjRawKarambwan      = 3142 // [tbwt_raw_karambwan]
	TbObjPoorKarambwan     = 3146 // [tbwt_poorly_cooked_karambwan]
	TbObjBurntKarambwan    = 3148 // [tbwt_burnt_karambwan]
	TbObjPoisonPaste       = 3153 // [tbwt_poisonous_karambwan_paste]
	TbObjMonkeyCorpse      = 3166 // [tbwt_monkey_corpse]
	TbObjMonkeySkin        = 3167 // [tbwt_monkey_skin]
	TbObjSandwich          = 3168 // [tbwt_seaweed_in_monkey_skin_sandwich]
	TbObjJogreBones        = 3125 // [tbwt_jogre_bones]
	TbObjBurntJogreBones   = 3127 // [tbwt_burnt_jogre_bones]
	TbObjBurntBonesInPaste = 3128 // [tbwt_burnt_jogre_bones_in_raw_karambwanji_paste]
	TbObjMarinatedBones    = 3130 // [tbwt_burnt_jogre_bones_marinated_in_karambwanji]
)

// TbSpear is a spear Tamayu accepts and the Karambwan-poisoned form it becomes. Bronze is left out: it sets no strength bit at any dose
// of poison. The (p) tiers are left out too: the paste lands only on the unpoisoned spears (make_tbwt_poisoned_weapon reads
// tbwt_weapon_poisoned, a param only they carry, spears.obj).
type TbSpear struct {
	Name         string
	ID, PoisonID int
}

// TbSpears are the spears above bronze, cheapest first (Tamayu keeps whatever he is given).
var TbSpears = []TbSpear{
	{"Iron spear", 1239, 3171}, {"Steel spear", 1241, 3172}, {"Mithril spear", 1243, 3173}, {"Adamant spear", 1245, 3174}, {"Rune spear", 1247, 3175},
}

// TbPotion is an agility potion bottle and the doses it carries.
type TbPotion struct {
	Name  string
	ID    int
	Doses int
}

// TbPotions are the agility potions, fullest first; Tamayu counts doses (~set_tbwt_tamayu_agility_count) and truncates at four.
var TbPotions = []TbPotion{
	{"Agility potion(4)", 3032, 4}, {"Agility potion(3)", 3034, 3}, {"Agility potion(2)", 3036, 2}, {"Agility potion(1)", 3038, 1},
}

// TbAgilityDoses is what Tamayu drinks before he can match the Shaikahan.
const TbAgilityDoses = 4

// TbKarambwanjiWanted is what Lubufu asks for.
const TbKarambwanjiWanted = 20

// TbMinFishingForKarambwan is the level the vessel is lowered at (attempt_fish_karambwan).
const TbMinFishingForKarambwan = 65

// TbSpearHeld is the id of a spear above bronze the pack holds (0 for none) and TbPoisonedHeld the Karambwan-poisoned one.
func TbSpearHeld(s Snapshot) int {
	for _, sp := range TbSpears {
		if s.HeldID(sp.ID) > 0 {
			return sp.ID
		}
	}
	return 0
}

// TbPoisonedSpearHeld is the poisoned spear in the pack, 0 for none.
func TbPoisonedSpearHeld(s Snapshot) int {
	for _, sp := range TbSpears {
		if s.HeldID(sp.PoisonID) > 0 {
			return sp.PoisonID
		}
	}
	return 0
}

// TbDosesHeld is the doses of agility potion in the pack across every bottle size.
func TbDosesHeld(s Snapshot) int {
	n := 0
	for _, p := range TbPotions {
		n += s.HeldID(p.ID) * p.Doses
	}
	return n
}

// TbPotionHeld is the fullest bottle in the pack, false for none.
func TbPotionHeld(s Snapshot) (TbPotion, bool) {
	for _, p := range TbPotions {
		if s.HeldID(p.ID) > 0 {
			return p, true
		}
	}
	return TbPotion{}, false
}

// Tiles, from the content maps (npc spawns) and the loc index.
var (
	// TbTimfrakuAnchor is the chief, on the first floor of the hut (m43_48 "1 28 15"); the ladder is at 2782,3087.
	TbTimfrakuAnchor = tile.Tile{X: 2780, Z: 3087, Level: 1}
	// TbLubufuAnchor is Lubufu (m43_49 "0 18 33"); the Karambwan spot he lets the vessel down at is 2768,3165.
	TbLubufuAnchor = tile.Tile{X: 2770, Z: 3169}
	// TbTiadecheAnchor is Tiadeche (m45_48 "0 32 46"), who does not wander; two Karambwan spots lie beside him (2912,3119, 2899,3119).
	TbTiadecheAnchor = tile.Tile{X: 2912, Z: 3118}
	// TbTamayuAnchor is Tamayu (m44_47 "0 28 34").
	TbTamayuAnchor = tile.Tile{X: 2844, Z: 3042}
	// TbTinsayAnchor is Tinsay (m43_46 "0 12 32"); two scorpions (level 14, hunters only of combat 28 and under) stand beside him.
	TbTinsayAnchor = tile.Tile{X: 2764, Z: 2976}
	// TbMonkeys is the middle of the six monkeys that stand east of the village's south road, a dozen tiles from Tamayu.
	TbMonkeys = tile.Tile{X: 2833, Z: 3031}
	// TbFire is the jungle fire's west tile; the fire is two tiles wide.
	TbFire = tile.Tile{X: 2789, Z: 3048}
	// TbLakeShoals are the Holy Lake's Karambwanji spots (0_43_47_karambwanji).
	TbLakeShoals = []tile.Tile{{X: 2801, Z: 3010}, {X: 2791, Z: 3019}, {X: 2807, Z: 3021}}
	// TbJogreStage is where the walk to the pen ends, 10 tiles west of its loneliest Jogre (2916,3062), on walkable ground and outside every fenced rectangle:
	// the fight itself walks the last stretch.
	TbJogreStage = tile.Tile{X: 2906, Z: 3068}
	// TbJogreTarget is the Jogre the kill is aimed at, 7 tiles from the nearest other and 14 from the nearest spider.
	TbJogreTarget = tile.Tile{X: 2916, Z: 3062}
)

// TbJiminuaShop is Jiminua's Jungle Store (junglestore, content karamja.inv): knife (2), pestle and mortar (3), tinderbox (2).
var TbJiminuaShop = ShopRef{Npc: "Jiminua", NpcID: TbNpcJiminua, Anchor: tile.Tile{X: 2767, Z: 3122}}

// The stops. The preferences are the options of the menus the stage reaches, matched as substrings in this order.
var (
	// TbTimfrakuStartStop: the title (any of four), "Trufitus sent me.", then the gratitude, then the job.
	TbTimfrakuStartStop = NpcStop{Npc: "Timfraku", ID: TbNpcTimfraku, Anchor: TbTimfrakuAnchor, Leash: 6,
		Prefer: []string{"I am a roving adventurer.", "Trufitus sent me.", "Your gratitude is all I deserve.", "Yes"}}
	// TbTimfrakuRewardStop: the brothers are home.
	TbTimfrakuRewardStop = NpcStop{Npc: "Timfraku", ID: TbNpcTimfraku, Anchor: TbTimfrakuAnchor, Leash: 6,
		Prefer: []string{"Oh it was nothing really.", "Ok, thanks."}}
	// TbLubufuMeetStop: the first talk is the brush-off whatever is chosen, the second walks the menus to the offer.
	TbLubufuMeetStop = NpcStop{Npc: "Lubufu", ID: TbNpcLubufu, Anchor: TbLubufuAnchor, Leash: 6,
		Prefer: []string{"What's a whippersnapper?", "Talk about him", "How old are you?", "I could help collect the bait.", "You sound like you could do with the help."}}
	// TbLubufuGiveStop: "Have you retrieved the Karambwanji yet?" has no menu when the pack holds some.
	TbLubufuGiveStop = NpcStop{Npc: "Lubufu", ID: TbNpcLubufu, Anchor: TbLubufuAnchor, Leash: 6,
		Prefer: []string{"What is a Karambwan?"}}
	// TbLubufuAskStop: after the hand-in he answers any three questions with the offer; "Yes!" accepts it.
	TbLubufuAskStop = NpcStop{Npc: "Lubufu", ID: TbNpcLubufu, Anchor: TbLubufuAnchor, Leash: 6,
		Prefer: []string{"Yes!", "What is a Karambwan?"}}
	// TbLubufuVesselStop: a vessel lost is a vessel given again (stage 31 shows the menu; stage 30 hands it over unasked).
	TbLubufuVesselStop = NpcStop{Npc: "Lubufu", ID: TbNpcLubufu, Anchor: TbLubufuAnchor, Leash: 6,
		Prefer: []string{"Actually, I've lost my Karambwan vessel.", "a shark ate it!", "Yes!", "I'm fine thanks."}}
	// TbTiadecheStop: the first talk (any option of the first menu; "When will you be finished?" is the one that changes nothing),
	// the catch (the loaded vessel used on him: "Yes" to his first catch), and every later talk.
	TbTiadecheStop = NpcStop{Npc: "Tiadeche", ID: TbNpcTiadeche, Anchor: TbTiadecheAnchor, Leash: 6,
		Prefer: []string{"When will you be finished?", "Yes"}}
	// TbTamayuStop: the vow, the hunts ("When will you succeed?" with "Yes" before the first, "Take me on your next hunt" after).
	TbTamayuStop = NpcStop{Npc: "Tamayu", ID: TbNpcTamayu, Anchor: TbTamayuAnchor, Leash: 6,
		Prefer: []string{"Take me on your next hunt", "When will you succeed?", "Yes"}}
	// TbTinsayStop: "Have you found it yet?" answered "Yes." when the item is in the pack.
	TbTinsayStop = NpcStop{Npc: "Tinsay", ID: TbNpcTinsay, Anchor: TbTinsayAnchor, Leash: 6,
		Prefer: []string{"Yes."}}
)
