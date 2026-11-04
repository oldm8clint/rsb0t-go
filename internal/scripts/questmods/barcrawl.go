package questmods

import (
	"time"

	"rsb0t-go/internal/scripts/barcrawlplan"
	"rsb0t-go/internal/scripts/barcrawlrunner"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
)

// Alfred Grimhand's Barcrawl on the engine: the miniquest questport ported and proved live
// (barcrawlplan/barcrawlrunner), wrapped as one custom step so the ladder can schedule it (the
// Barbarian Outpost gate it opens is on the way to Scorpion Catcher's scorpion B and Horror from
// the Deep's key). It has no quest tab: the gate guard's greeting is the server's verdict
// ("'Ello friend." once complete), and the ladder's Done reads the store's record of it.
//
// THE CARD LIVES IN THE BANK BETWEEN RUNS. Everything that runs between two barcrawl runs (the Stage 5 trainer, the
// strips of the quests before it) banks what it does not keep, and none of them knows the card, so a crawl that one run
// started (or one the slot cut short) resumes with the card in the bank. The guard issues a card only while neither the
// pack nor the bank holds one ("Not too bad, my barcrawl card is in my bank now" and nothing handed over), so the module
// has to draw it from there before the tour: live 13-15 Oct 2026, 2,196 guard talks in the logs scanned all ended with no
// card, and every bank read afterwards held it. decideBarcrawl therefore reads the bank first (the engine's scan step) and withdraws
// the card (its withdraw step), and every bank visit it asks for, like every one the engine makes while the quest runs
// (Module.ToolIDs), leaves the card and the coins in the pack.

// BarcrawlID is the ladder's id.
const BarcrawlID = "barcrawl"

const customBarcrawl = "barcrawl-ensure"

// KitBarcrawlCoins is the coin kit's name, the key of its Snapshot.Ran and Failed entries ("provision:" + the name).
const KitBarcrawlCoins = "barcrawl-coins"

// barcrawlRecord is the miniquest's identity (0 QP).
var barcrawlRecord = questplan.Record{ID: BarcrawlID, Name: "Alfred Grimhand's Barcrawl"}

// barcrawlKeep is what a barcrawl bank visit leaves in the pack: the card, which the guard will not reissue while a copy
// sits in the bank, and the coins the ten drinks and the fares are paid from.
var barcrawlKeep = []int{barcrawlplan.CardID, questplan.CoinsID}

// decideBarcrawl is the whole miniquest. With no card in the pack it pays for the drinks first (the coin ask), then
// reads the bank and draws the card out of it; only then does the custom step walk to the guard and the bars.
func decideBarcrawl(s questplan.Snapshot) questplan.Step {
	if s.Ran[customBarcrawl] {
		return questplan.Step{Kind: questplan.StepDone}
	}
	if barcrawlCardHeld(s) > 0 {
		// A card in the pack means the coins are already out for the tour: the drinks spend them down as it goes.
		return questplan.Step{Kind: questplan.StepCustom, Custom: customBarcrawl, Reason: "card, ten bars, hand-in"}
	}
	bank, bankSet := barcrawlBank(s)
	// The coin ask (the item rule: own bank, then one bundled ask to the mule), BEFORE the card is drawn: the drinks cost
	// 208 gp and the ship fares between them 448 gp in all on the first full tour, so the pack leaves for the tour with
	// CoinFloat, the card still safe in the bank. The provision step's bank visit banks whatever the kit and barcrawlKeep
	// do not claim, which the card (if it were held) is.
	if s.HeldID(questplan.CoinsID) < barcrawlplan.CoinFloat && !s.Ran["provision:"+KitBarcrawlCoins] {
		return questplan.Step{Kind: questplan.StepProvision, KitName: KitBarcrawlCoins, Kit: []questplan.KitLine{{
			ID: questplan.CoinsID, Name: "Coins", Qty: barcrawlplan.CoinFloat, Stackable: true,
		}}, KeepIDs: barcrawlKeep, Bank: bank, BankSet: bankSet, Reason: "the ten drinks and the Karamja fares"}
	}
	banked, known := s.BankedID(barcrawlplan.CardID)
	switch {
	case !known:
		// A bank view older than this run is not trusted (Engine.Snapshot), so the first tick of every run reads it. One
		// trip, and the coin ask above has usually made it already.
		return questplan.Step{Kind: questplan.StepScanBank, KeepIDs: barcrawlKeep, Bank: bank, BankSet: bankSet}
	case banked > 0:
		return questplan.Step{Kind: questplan.StepWithdraw, Items: []questplan.WithdrawItem{{
			Name: barcrawlplan.CardName, ID: barcrawlplan.CardID, Qty: 1,
		}}, KeepIDs: barcrawlKeep, Bank: bank, BankSet: bankSet}
	}
	// No card anywhere: not started, or lost. The guard issues one in both cases.
	return questplan.Step{Kind: questplan.StepCustom, Custom: customBarcrawl, Reason: "card, ten bars, hand-in"}
}

// barcrawlBank is the bank a barcrawl visit uses: the one nearest the account (the Barbarian Outpost, the ten bars and
// the guard are all over the map, and the walk is the cost). BankSet is false before a position packet has arrived.
func barcrawlBank(s questplan.Snapshot) (tile.Tile, bool) {
	if !s.TileKnown {
		return tile.Tile{}, false
	}
	return questplan.NearestBank(s.Tile).Tile, true
}

// barcrawlCardHeld counts the card in the pack.
func barcrawlCardHeld(s questplan.Snapshot) int { return s.HeldID(barcrawlplan.CardID) }

// barcrawlTour is the proven tour (a seam for the tests: the live one walks the map).
var barcrawlTour = barcrawlrunner.Ensure

// Barcrawl is the Barcrawl ready for the engine.
func Barcrawl() questrunner.Quest {
	return questrunner.Quest{
		Module: questplan.Module{Record: barcrawlRecord, Bank: questplan.DraynorBank, BankSet: false,
			// By name and by id: the engine's own deposits (a withdrawal that needs room, a provisioning round) leave
			// the card and the coins in the pack while this quest runs.
			Tools: []string{"barcrawl card"}, ToolIDs: barcrawlKeep, Decide: decideBarcrawl},
		Customs: map[string]questrunner.CustomFunc{customBarcrawl: barcrawlEnsure},
		Done: func(e *questrunner.Engine) bool {
			return e.Store != nil && e.Store.Record(BarcrawlID).SeenComplete
		},
	}
}

// barcrawlEnsure runs the proven tour and records the guard's verdict.
func barcrawlEnsure(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	logf(d, "barcrawl: starting with %d card(s) and %d coins in the pack", questrunner.HeldID(d, barcrawlplan.CardID), questrunner.HeldID(d, questplan.CoinsID))
	ok := barcrawlTour(d, func(signed, total int) {
		logf(d, "barcrawl: %d of %d bars signed", signed, total)
	})
	if ok && e.Store != nil {
		r := e.Store.Record(BarcrawlID)
		r.SeenComplete = true
		r.UpdatedAt = time.Now().UnixMilli()
	}
	return ok
}
