// Package barcrawlplan is the pure half of Alfred Grimhand's Barcrawl, ported from the
// TypeScript client's src/bot/api/ai/quests/barcrawl/BarcrawlLogic.ts. It is pure: no client,
// actions, fleet or bot imports and no I/O.
//
// # Why this is its own package and not a questplan Module
//
// The Barcrawl is a MINIQUEST. It is not in the quest tab at all — content 289's
// player/interfaces/questlist.if has no `barcrawl` entry, so internal/quests.Status has
// nothing to read and the questplan engine's whole state model (journal colour plus held
// items) does not apply. Upstream reached the same conclusion: data/quests.ts carries no
// record for it, and the driver lives outside defs/.
//
// It is ported anyway, and first, because it is the gate in front of a script this repo
// already runs. content/scripts/areas/area_barbarian_outpost/scripts/outpost_gate.rs2 answers
// OPLOC1 with
//
//	if(%barcrawl ! ^barcrawl_complete) { if(npc_find(coord, barbguard1, 5, 0)) { @outpost_guard_talk } }
//
// and the two Barbarian guards stand on the gate tiles themselves, so for an account that has
// not finished the crawl the click opens the guard's chat dialogue and the gate never opens —
// exactly the behaviour internal/webwalk/walker.go's ModalClient comment records as found live
// on 2026-09-18 at the outpost's west gates (locs 2116 at 2545,3569 and 2115 at 2545,3570).
//
// # The oracle, and why it is the card rather than a varp
//
// content/scripts/quests/quest_barcrawl/configs/quest_barcrawl.varp is `[barcrawl] scope=perm`
// with no `transmit=yes`, so which bars are signed is never on the wire. The card's own Read
// op is the only reading there is: opheld1 on barcrawl_card runs
// general/scripts/scroll.rs2's `scroll_barcrawl_card`, which writes one green "Completed!" or
// red "Not Completed..." line per bar into the shared `scroll` main modal with if_settext.
// That is the same shape as a quest journal and is read the same way.
//
// # The oracle stops answering when it succeeds
//
// quest_barcrawl.rs2's opheld1 checks `~visited_all_bars` FIRST and, when every bar is signed,
// prints "You are too drunk to be able to read the barcrawl card" and returns WITHOUT opening
// the scroll. Reading that as a failed read leaves the tour looping on the tenth bar forever.
// It is the finished state, and the refusal message is what says so — hence TooDrunk below,
// which a caller must check before it calls a read failed.
//
// # Deliberately not ported
//
//   - The driving half (RunBarcrawl.ts) — that is I/O and lives in barcrawlrunner.
//   - The Electron wall's progress paint. HUD publishers are never ported.
package barcrawlplan

import (
	"regexp"
	"sort"
	"strings"

	"rsb0t-go/internal/scripts/tile"
)

// CardName is the barcrawl card's display name, from
// quests/quest_barcrawl/configs/quest_barcrawl.obj.
const CardName = "Barcrawl card"

// CardID is barcrawl_card (content/pack/obj.pack 455). A module that keeps the card in the pack, or draws it back out of
// the bank, names it by this id: the display name is shared with nothing, but an id cannot be matched by accident.
const CardID = 455

// ReadOp is the card's own inventory op (`iop1=Read` in that same .obj).
const ReadOp = "Read"

// CoinsName is the coin stack's display name.
const CoinsName = "Coins"

// CoinsID is the coin object id.
const CoinsID = 995

// Bar is one stop on the tour.
type Bar struct {
	// ID is the exact NPC id. Bars are found by ID and never by name: seven of the ten
	// bartenders below render the identical display name "Bartender" in this content
	// build, so a name lookup would pick whichever happened to be nearest.
	ID int
	// Npc is the display name, for logs only.
	Npc string
	// Tile is where that bartender stands.
	Tile tile.Tile
	// Line is the fragment of this bar's card line, lower-cased, that identifies it.
	Line string
	// GP is what that bar's barcrawl drink costs.
	GP int
}

// Bars is the ten stops. Every ID below was checked against content/pack/npc.pack for this
// build (568=zambo, 731=jollyboar_bartender, 733=bluemoon_bartender, 734=rustyanchor_bartender,
// 735=deadmans_bartender, 736=risingsun_barmaid, 737=foresters_bartender,
// 738=flyinghorse_bartender, 739=dragon_bartender, 848=blurberry), and every GP against that
// bar's own script — e.g. areas/area_falador/scripts/barmaid.rs2 refuses under 70 coins for the
// Rising Sun's Hand of Death.
var Bars = []Bar{
	{ID: 734, Npc: "Bartender", Tile: tile.Tile{X: 3045, Z: 3258, Level: 0}, Line: "rusty anchor", GP: 8},
	{ID: 736, Npc: "Barmaid", Tile: tile.Tile{X: 2957, Z: 3371, Level: 0}, Line: "rising sun", GP: 70},
	{ID: 733, Npc: "Bartender", Tile: tile.Tile{X: 3226, Z: 3400, Level: 0}, Line: "bluemoon", GP: 50},
	{ID: 731, Npc: "Bartender", Tile: tile.Tile{X: 3276, Z: 3488, Level: 0}, Line: "jolly boar", GP: 10},
	{ID: 737, Npc: "Bartender", Tile: tile.Tile{X: 2690, Z: 3494, Level: 0}, Line: "foresters arms", GP: 18},
	{ID: 738, Npc: "Bartender", Tile: tile.Tile{X: 2575, Z: 3321, Level: 0}, Line: "flying horse", GP: 8},
	{ID: 739, Npc: "Bartender", Tile: tile.Tile{X: 2556, Z: 3079, Level: 0}, Line: "dragon inn", GP: 12},
	{ID: 848, Npc: "Blurberry", Tile: tile.Tile{X: 2480, Z: 3490, Level: 1}, Line: "blurberry's bar", GP: 10},
	{ID: 568, Npc: "Zambo", Tile: tile.Tile{X: 2924, Z: 3143, Level: 0}, Line: "karamja spirits", GP: 7},
	{ID: 735, Npc: "Bartender", Tile: tile.Tile{X: 2796, Z: 3155, Level: 0}, Line: "dead man's chest", GP: 15},
}

// CoinFloat is every drink plus slack for the boat fares and the walk.
//
// The drinks themselves total 208. Upstream's BARCRAWL_GP adds 200 for "slack", and that is
// **not enough on this build**: the first full live tour (cmd/barcrawltest, 2026-09-21, PASS)
// spent **448gp** end to end, because the routing between Yanille, Ardougne and Varrock takes
// the Karamja ship chain more than once at 30gp a crossing. 408 would have come up 40gp short
// on that very run, and running out mid-tour strands the account with a half-signed card and no
// way to buy the next drink.
//
// 800 is therefore the float: the measured 448, plus room for a route that crosses a few more
// times than that one did. It is deliberately not tuned finer, because the cost is a routing
// outcome rather than a fixed price list, and the downside of too little is far worse than the
// downside of too much.
var CoinFloat = totalGP() + 592

func totalGP() int {
	sum := 0
	for _, b := range Bars {
		sum += b.GP
	}
	return sum
}

// GuardID is barbguard1, the gate guard — 384 in content/pack/npc.pack.
//
// He is found by ID rather than by name on purpose: the outpost's other "Barbarian guard" is
// the attackable one, and the gate loc runs the same conversation, so a talk aimed by display
// name can land on either.
const GuardID = 384

// GuardName is his display name, for logs.
const GuardName = "Barbarian guard"

// GuardTile is where the gate guard stands.
var GuardTile = tile.Tile{X: 2544, Z: 3572, Level: 0}

// GuardPrefer drives outpost_guard_start's two menus. The branch that issues a card is two
// menus deep, so stopping at the first lull leaves the choice on screen with the tour
// un-started.
var GuardPrefer = []string{
	"I want to come through this gate.",
	"Looks can be deceiving, I am in fact a barbarian.",
}

// BarPrefer is barcrawl-first, so an ale menu never wins the pick.
//
// Nine of the ten bars go straight into the drink when the card is used on them, but the
// Rising Sun's barmaid offers her ale list first (areas/area_falador/scripts/barmaid.rs2's
// p_choice2), so the barcrawl line has to be in the preference list or her menu sits
// unanswered and the card never signs.
var BarPrefer = []string{"Alfred Grimhand", "Barcrawl", "Yes"}

// TooDrunk is the card's own refusal once all ten lines are green — quest_barcrawl.rs2's
// `mes("You are too drunk to be able to read the barcrawl card")`. It is the finished state,
// not a failed read.
var TooDrunk = regexp.MustCompile(`(?i)too drunk to be able to read`)

// GateIsOpen is outpost_guard_talk's greeting for `%barcrawl = ^barcrawl_complete`: its
// `case 2` is `~chatnpc("<p,happy>'Ello friend.")`.
//
// The apostrophe is deliberately not in the pattern, because the client's own rendering of a
// leading apostrophe is not something to bet a verdict on. Matching "ello friend" is still
// unambiguous against the other two greetings, "Oi, whaddya want?" and "So, how's the Barcrawl
// coming along?".
var GateIsOpen = regexp.MustCompile(`(?i)ello friend`)

// GateIsUnderway is outpost_guard_progress's opening line, the greeting for any `%barcrawl` that is neither 0 (not
// started) nor 2 (complete): `~chatnpc("<p,quiz>So, how's the Barcrawl coming along?")`.
//
// What follows it in barbarian_outpost_guard.rs2 is decided by where the card is, and that is what makes this line
// evidence about the card and not only about the crawl:
//
//	pack and bank both empty -> "I've lost my barcrawl card..." and the guard hands over a NEW card;
//	bank holds one, pack none -> "my barcrawl card is in my bank now" and the guard hands over NOTHING;
//	pack holds one            -> "I haven't finished it yet" (or the hand-in once every bar is signed).
//
// So an account that opens with this line, holds no card and is handed none has its card in the bank. Live, the fleet
// logs of 13-15 Oct 2026: 2,196 guard talks in the 14 logs scanned all ended exactly there ("cards in pack: 0", none a
// card issued), and the three account banks read afterwards each held the card (obj 455).
var GateIsUnderway = regexp.MustCompile(`(?i)barcrawl coming along`)

// Progress is how much of the card is signed.
type Progress struct {
	// Remaining is the bars whose card line is still red, in Bars order.
	Remaining []Bar
	// Done is true once every line is green.
	Done bool
}

// colourTag matches the client's inline colour markup, replaced with a space so words either
// side of it do not run together.
var colourTag = regexp.MustCompile(`(?i)@[a-z0-9]{3}@`)

// whitespaceRun folds pipes (the client's own in-string line break) and whitespace together.
var whitespaceRun = regexp.MustCompile(`[|\s]+`)

// normalise flattens the scroll's rendered lines into one lower-cased string.
func normalise(lines []string) string {
	joined := strings.Join(lines, " ")
	joined = colourTag.ReplaceAllString(joined, " ")
	joined = whitespaceRun.ReplaceAllString(joined, " ")
	return strings.ToLower(strings.TrimSpace(joined))
}

// lineWindow is how far past a bar's needle to look for its verdict.
//
// The scroll writes "<Bar name> - Completed!" or "<Bar name> - Not Completed...", so the
// verdict sits a fixed short distance after the needle. Checked against every one of the ten
// rendered lines in this content build: the longest gap between the end of a needle and the
// end of "not completed" is 23 characters ("rusty anchor" -> "rusty anchor inn - not
// completed"), so 24 covers all ten with a character to spare while staying far too short to
// reach the NEXT bar's line and read its verdict by mistake.
const lineWindow = 24

// notCompleted is the negation each line is matched on.
//
// Matching the negation rather than the happy word is not a style choice: "Completed!" and
// "Not Completed..." BOTH contain "completed", so a positive match reports every bar as signed
// from the very first read.
const notCompleted = "not completed"

// ParseCard reads the rendered card scroll. ok is false when the text is not a barcrawl card
// at all — an empty read, a stale modal, or somebody else's scroll — which a caller must not
// confuse with "no bars signed".
func ParseCard(lines []string) (Progress, bool) {
	text := normalise(lines)
	if !strings.Contains(text, "barcrawl") {
		return Progress{}, false
	}
	var remaining []Bar
	for _, bar := range Bars {
		at := strings.Index(text, bar.Line)
		if at == -1 {
			remaining = append(remaining, bar)
			continue
		}
		end := at + len(bar.Line) + lineWindow
		if end > len(text) {
			end = len(text)
		}
		if strings.Contains(text[at:end], notCompleted) {
			remaining = append(remaining, bar)
		}
	}
	return Progress{Remaining: remaining, Done: len(remaining) == 0}, true
}

// NextBar picks the nearest outstanding bar, so the tour is a rough loop rather than the list
// order. ok is false when nothing is outstanding.
//
// Ties are broken by the bar's ID so the choice is deterministic; without that, two bars the
// same distance away would be picked by Go's sort stability rather than by anything meaningful,
// and a test could not pin the behaviour.
func NextBar(remaining []Bar, here tile.Tile, hereKnown bool) (Bar, bool) {
	if len(remaining) == 0 {
		return Bar{}, false
	}
	if !hereKnown {
		return remaining[0], true
	}
	sorted := make([]Bar, len(remaining))
	copy(sorted, remaining)
	sort.SliceStable(sorted, func(i, j int) bool {
		di, dj := tile.Chebyshev(sorted[i].Tile, here), tile.Chebyshev(sorted[j].Tile, here)
		if di != dj {
			return di < dj
		}
		return sorted[i].ID < sorted[j].ID
	})
	return sorted[0], true
}

// Signed reports whether a given bar is signed in a parsed progress.
func (p Progress) Signed(bar Bar) bool {
	for _, r := range p.Remaining {
		if r.ID == bar.ID {
			return false
		}
	}
	return true
}

// SignedCount is how many of the ten are green.
func (p Progress) SignedCount() int { return len(Bars) - len(p.Remaining) }

// GuardVerdict is what the gate guard's greeting says about the crawl.
type GuardVerdict string

const (
	// VerdictComplete means the guard waves the account through: the crawl is done.
	VerdictComplete GuardVerdict = "complete"
	// VerdictIssued means a card is now in the pack.
	VerdictIssued GuardVerdict = "issued"
	// VerdictRetry means the conversation was cut short and proved nothing.
	VerdictRetry GuardVerdict = "retry"
	// VerdictBanked means the crawl is under way, the pack holds no card and the guard handed none over: by the
	// guard's own script (GateIsUnderway) the card is in the bank, and only drawing it from there continues the crawl.
	VerdictBanked GuardVerdict = "banked"
)

// CrawlUnderway reports whether the guard opened with outpost_guard_progress's greeting: the crawl has been started
// (and is not complete). It proves nothing about the card by itself; see GateIsUnderway.
func CrawlUnderway(lines []string) bool {
	return GateIsUnderway.MatchString(normalise(lines))
}

// ReadGreeting classifies the gate guard's opening line.
//
// The greeting is the verdict because `%barcrawl` is not on the wire and an empty pack looks
// the same before a card is issued as after one is handed in. outpost_guard_talk branches on
// the varp: "Oi, whaddya want?" for not started, "'Ello friend." for complete, and "So, how's
// the Barcrawl coming along?" for anything in between.
//
// Reading "no card came out" as "already done" would send the bot at a gate that will not
// open, so ONLY the complete greeting returns VerdictComplete; everything else is left for the
// caller to resolve by driving the conversation and looking at the pack.
func ReadGreeting(lines []string) (GuardVerdict, bool) {
	text := normalise(lines)
	if text == "" {
		return VerdictRetry, false
	}
	if GateIsOpen.MatchString(text) {
		return VerdictComplete, true
	}
	return VerdictRetry, true
}
