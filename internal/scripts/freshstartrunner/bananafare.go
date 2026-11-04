package freshstartrunner

import (
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/scripts/freshstart"
	"rsb0t-go/internal/scripts/tile"
)

// Content 289's quest_hunt/luthas.rs2 pays 30 coins for ten bananas. Neither
// employment nor crate fill is sent to the client as a varp, so recovery must
// confirm inventory changes and drive the real dialogue, including after a restart.
type bananaFare struct {
	active  bool
	working bool
	packed  int
}

// m45_49.jm2 places Luthas at (2939,3154) and the crate at (2943,3151).
// WalkNear handles the mainland gate when approaching from Brimhaven.
var plantation = tile.Tile{X: 2939, Z: 3154}

// Content 289 loc.pack: 2073..2077 bear fruit; 2078 is empty but
// still exposes Search. Naming the option alone would repeatedly select it.
func bananaTreeQuery(here tile.Tile) locindex.Query {
	return locindex.Query{X: here.X, Z: here.Z, Level: 0, Radius: 25,
		IDs: []int{2073, 2074, 2075, 2076, 2077}, Op: "Search"}
}

// needsBananaFare: on Karamja, short of the fare, and no pickpocket target in reach. A visible
// Man always wins, even over a banana job already started (active is kept for the callers'
// bookkeeping only). Lab 2026-09-20 at the live 600ms tick (cmd/labfleettest -mode
// karamja-fare): three combat-3 fishers stranded at the Brimhaven dock all reached the
// plantation alive and none earned a coin in 12 minutes, standing beside Luthas with the Musa
// Point Man (content m46_49, 2948,3146) in view: bananas were picked but the crate never took
// one, and the old "|| active" kept them on the job regardless. The same account pickpocketing
// that Man earned coins within minutes. The banana crate has never been proven on an engine
// (COLLAB 2026-09-17 said so); pickpocketing has.
func needsBananaFare(here *tile.Tile, coins, need, target int, active bool) bool {
	_ = active
	return here != nil && freshstart.OnKaramjaSide(*here) && coins < need && target == -1
}

// hiringChoice names the two dialogue branches that actually set %hunt_store_employed in
// content 289's luthas.rs2 (luthas_employment and luthas_another). "What did I have to do
// again?" repeats the instructions and hires nobody.
func hiringChoice(want string) bool {
	return want == "offer me employment" || want == "pay me for another crate"
}

// crateRefusedEmployment is the crate's own mes() for an account Luthas has not hired
// (quest_hunt/banana_crate.rs2). It is an ordinary game message, so it only ever reaches
// this script through the chat log — which is exactly why the lab runs looked like a silent
// OPLOCU failure: the server answered every time and nothing here read the answer.
const crateRefusedEmployment = "why would i want to do that"

func bananaJobChoice(options []actions.Option, paid bool, enough bool) string {
	wanted := []string{"offer me employment", "pay me for another crate", "what did i have to do again"}
	if paid && enough {
		wanted = []string{"thank you, i'll be on my way"}
	}
	for _, want := range wanted {
		for _, option := range options {
			if strings.Contains(strings.ToLower(option.Text), want) {
				return want
			}
		}
	}
	return ""
}

func bananaFareStep(d *Deps, job *bananaFare, need int) {
	job.active = true
	if d.Pack == nil || d.Locs == nil || d.Config == nil || d.Components == nil {
		d.logf("Karamja fare recovery needs collision, loc and interface data — cannot walk to a mainland fare spot across a paid ship")
		delayWatchingCombat(d, 30*time.Second)
		return
	}
	here := currentTileOrZero(d)
	if here.Level != 0 || tile.Chebyshev(here, plantation) > 15 {
		d.logf("short of the boat fare — walking to Luthas's plantation to earn coins locally")
		// walkWithWalker, not a bare WalkNear: real, live-reproduced gap (account "acct5"/
		// "wario", stranded at the Brimhaven pier, 2772,3235,0) — WalkNear alone reported
		// "no path ... unreachable" to the plantation every single attempt despite both
		// tiles genuinely sharing Karamja's own landmass (no ship needed for this leg, per
		// this file's own "WalkNear handles the mainland gate" comment above), because the
		// static pathfind pack has no notion of a closed gate/door the account hasn't
		// opened yet along this specific route. See walkWithWalker's own doc comment.
		if !walkWithWalker(d, plantation, 3, 90*time.Second) {
			delayWatchingCombat(d, 30*time.Second)
		}
		return
	}
	if !job.working || job.packed >= 10 {
		if talkBananaJob(d, need) {
			job.working = true
			job.packed = 0
		}
		return
	}
	eq := actions.NewEquipment(d.Session, d.Components)
	var bananaID int32
	hasBanana := false
	for _, item := range eq.Inventory() {
		obj, ok := d.Config.Obj(int(item.ID))
		if ok && !obj.Noted() && strings.EqualFold(obj.Name, "Banana") {
			bananaID, hasBanana = item.ID, true
			break
		}
	}
	live := locindex.ChangesFrom(d.Session.State().LocChanges())
	query := bananaTreeQuery(here)
	if hasBanana {
		// Restrict to the plantation crate: many unrelated locs are named Crate.
		query = locindex.Query{X: 2943, Z: 3151, Level: 0, Radius: 0, IDs: []int{2072}}
	} else if len(eq.Inventory()) >= 28 {
		d.logf("Karamja fare recovery needs one free inventory slot for a banana")
		delayWatchingCombat(d, 30*time.Second)
		return
	}
	match, ok := d.Locs.Nearest(d.Config, query, live)
	if !ok {
		d.logf("Karamja fare recovery: no productive banana tree or plantation crate available")
		d.Delay(5 * time.Second)
		return
	}
	// Reach an orthogonal neighbour exactly; radius walks can stop diagonally,
	// from which the engine may accept but never execute the interaction.
	if !approachBananaLoc(d, match) {
		delayWatchingCombat(d, 5*time.Second)
		return
	}
	if err := actions.CloseModal(d.Session); err != nil {
		d.Delay(time.Second)
		return
	}
	before := bananaCount(d)
	clickAt := time.Now()
	var err error
	if hasBanana {
		err = eq.UseOnLoc(bananaID, match.X, match.Z, match.ID)
	} else {
		err = actions.OpLoc(d.Session, match.Op, match.X, match.Z, match.ID)
	}
	if err != nil {
		d.logf("Karamja banana interaction: %v", err)
		d.Delay(time.Second)
		return
	}
	changed := waitFor(d, "the banana inventory update", 5*time.Second, func() bool {
		now := bananaCount(d)
		if hasBanana {
			return now < before
		}
		return now > before
	})
	if hasBanana {
		if changed {
			job.packed++
		} else {
			// The crate refusing a banana means one of two things, and the server says
			// which: "Why would I want to do that?" is Luthas never having hired this
			// account, and anything else (in practice a full crate a previous process
			// left behind) is a crate to claim. Either way the answer is to go back to
			// Luthas, but only the first is worth naming in the log — it used to be
			// read as OPLOCU doing nothing at all.
			if d.Chat != nil && strings.Contains(strings.ToLower(d.Chat.Since(clickAt)), crateRefusedEmployment) {
				d.logf("the plantation crate refused a banana because Luthas has not hired this account — going back to ask for the job")
			}
			job.working = false
		}
	}
	d.Delay(600 * time.Millisecond)
}

func bananaCount(d *Deps) int {
	n := 0
	for _, item := range actions.NewEquipment(d.Session, d.Components).Inventory() {
		obj, ok := d.Config.Obj(int(item.ID))
		if ok && !obj.Noted() && strings.EqualFold(obj.Name, "Banana") {
			n += int(item.Count)
		}
	}
	return n
}

func approachBananaLoc(d *Deps, match locindex.Match) bool {
	here := currentTileOrZero(d)
	typ, ok := d.Config.Loc(match.ID)
	if !ok {
		return false
	}
	approaches := bananaApproaches(match, typ)
	for _, to := range approaches {
		if here == to {
			return true
		}
	}
	for _, to := range approaches {
		if WalkNear(d, to, 0, 5*time.Second) {
			return true
		}
	}
	return false
}

func bananaApproaches(match locindex.Match, typ cachecfg.Loc) []tile.Tile {
	w, h := typ.Width, typ.Length
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	if match.Angle&1 != 0 {
		w, h = h, w
	}
	var out []tile.Tile
	for z := 0; z < h; z++ {
		out = append(out, tile.Tile{X: match.X - 1, Z: match.Z + z, Level: match.Level}, tile.Tile{X: match.X + w, Z: match.Z + z, Level: match.Level})
	}
	for x := 0; x < w; x++ {
		out = append(out, tile.Tile{X: match.X + x, Z: match.Z - 1, Level: match.Level}, tile.Tile{X: match.X + x, Z: match.Z + h, Level: match.Level})
	}
	return out
}

// One bounded dialogue transaction. Select only the known job branches, never
// an arbitrary first option. Coin gain is confirmed before declaring a payment.
func talkBananaJob(d *Deps, need int) bool {
	npc, ok := findNpcByNameOp(d, "Luthas", plantation, 10, "Talk-to")
	if !ok {
		WalkNear(d, plantation, 2, 20*time.Second)
		d.Delay(2 * time.Second)
		return false
	}
	if !WalkNear(d, tile.Tile{X: npc.X, Z: npc.Z, Level: npc.Level}, 2, 10*time.Second) {
		d.Delay(2 * time.Second)
		return false
	}
	if err := actions.CloseModal(d.Session); err != nil {
		d.Delay(time.Second)
		return false
	}
	npcType, _ := d.Config.Npc(npc.Type)
	before := coinsHeld(d)
	if err := actions.OpNpc(d.Session, cachecfg.OpIndex(npcType.Ops, "Talk-to"), npc.Index); err != nil {
		d.Delay(time.Second)
		return false
	}
	dlg := actions.NewChatDialog(d.Session, d.Components)
	// hired, not "selected something": content 289's luthas.rs2 sets %hunt_store_employed
	// from exactly two branches, luthas_employment ("Could you offer me employment...") and
	// luthas_another ("Will you pay me for another crate full?"). Every other branch is small
	// talk, and without that bit the crate answers "Why would I want to do that?" and takes
	// nothing. Treating any selection as a hire is what sent three lab accounts to pack
	// bananas into a crate that would never accept one (2026-09-20, 0 coins in 12 minutes) —
	// the crate was never broken, and neither was OPLOCU.
	seen, hired := false, false
	defer actions.CloseModal(d.Session)
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); {
		paid := coinsHeld(d) > before
		if paid && coinsHeld(d) >= need {
			d.logf("Luthas paid for the banana crate — boat fare now %d coins", coinsHeld(d))
			return true
		}
		if options := dlg.Options(); len(options) > 0 {
			seen = true
			want := bananaJobChoice(options, paid, coinsHeld(d) >= need)
			if want == "" {
				d.logf("Luthas offered an unrecognised dialogue — leaving it closed for a later retry")
				return false
			}
			if err := dlg.ChooseOption(want); err != nil {
				return false
			}
			if hiringChoice(want) {
				hired = true
			}
		} else if dlg.CanContinue() {
			seen = true
			if err := dlg.Continue(); err != nil {
				return false
			}
		} else if seen && !dlg.IsOpen() {
			if !hired {
				d.logf("Luthas's dialogue closed without an employment offer — not packing bananas into a crate that will refuse them")
			}
			return hired
		}
		if !d.Delay(600 * time.Millisecond) {
			return false
		}
	}
	d.logf("Luthas dialogue timed out — retrying the local banana job later")
	d.Delay(2 * time.Second)
	return false
}
