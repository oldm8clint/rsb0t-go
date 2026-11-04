package controlpanel

import (
	"math"
	"net/http"
	"strconv"

	"rsb0t-go/internal/bankxp"
)

// maxBankAllAccounts bounds the response the same way the old loop did.
const maxBankAllAccounts = 2048

// handleBankAll answers every account's bank for the combined-bank panel.
//
// Prefers BankAllSource, which answers from ONE fleet snapshot. The per-account fallback
// below is what this endpoint used to do for every Source, and it is quadratic: it asks for
// the account list (one whole-fleet snapshot) and then calls Bank(name) per account, each of
// which took another whole-fleet snapshot to find one name. Measured at the live roster:
// 267 snapshots for a single request, from a panel that refreshes every five seconds, each
// snapshot taking every live session's gamestate lock about eight times. That is the
// starvation behind CLAUDE.md's "status requests intermittently time out" — /api/status
// needs those same locks and queued behind it. See snapshotcost_test.go, which asserts the
// call counts rather than wall-clock so the guard stays meaningful on any machine.
//
// The fallback is kept rather than deleted because a Source that cannot do one pass (an
// in-memory fake, a future partial Source) must still work; it is simply no longer what the
// real fleet uses.
func (s *Server) handleBankAll(w http.ResponseWriter, r *http.Request) {
	if !getOnly(w, r) {
		return
	}
	source, ok := s.src.(BankSource)
	if !ok {
		writeError(w, http.StatusNotImplemented, "this fleet does not report bank contents")
		return
	}
	// The AccountBank list is kept alongside the wire bodies because it carries the per-account
	// skill levels the banked-experience gate needs, read out of the SAME snapshot. Reading
	// them back through Accounts() instead would cost a second whole-fleet pass per refresh,
	// which is the regression this endpoint exists to have fixed.
	source0 := make([]AccountBank, 0)
	banks := make([]bankBody, 0)
	if all, ok := s.src.(BankAllSource); ok {
		for _, b := range all.AllBanks() {
			if len(banks) >= maxBankAllAccounts {
				break
			}
			b.Items = clampSlots(b.Items)
			source0 = append(source0, b)
			banks = append(banks, bankBody{b.Account, b.Items, b.UpdatedAtUnixMs})
		}
	} else {
		for i, account := range s.src.Accounts(nil) {
			if i >= maxBankAllAccounts {
				break
			}
			items, updated, found := source.Bank(account.Name)
			if found {
				items = clampSlots(items)
				levels := map[string]int{}
				for name, c := range account.Skills {
					levels[name] = c.Level
				}
				if len(levels) == 0 {
					levels = nil
				}
				source0 = append(source0, AccountBank{Account: account.Name, Items: items, UpdatedAtUnixMs: updated, Levels: levels})
				banks = append(banks, bankBody{account.Name, items, updated})
			}
		}
	}
	// Value every DISTINCT item in the answer once (see bankvalue.go). This is per-item, not
	// per-slot and not per-account, so it costs the same whether one bank is open or 269 are,
	// and it takes no fleet snapshot of its own — the whole point of BankAllSource above.
	prices, priceInfo := s.valueBanks(banks)
	writeJSON(w, http.StatusOK, struct {
		Banks     []bankBody    `json:"banks"`
		Prices    []priceEntry  `json:"prices"`
		PriceBook priceBookInfo `json:"priceBook"`
		XP        bankXPBody    `json:"xp"`
	}{banks, prices, priceInfo, s.bankXPFor(source0, bankAllPolicy(r), r.URL.Query().Get("levels") == "current", bankAllRate(r))})
}

// bankAllPolicy reads the ?xp= route policy. Anything unrecognised is the fleet's own routes,
// which is the conservative answer: it counts only what a registered script actually runs.
func bankAllPolicy(r *http.Request) bankxp.Policy {
	if r.URL.Query().Get("xp") == string(bankxp.PolicyBest) {
		return bankxp.PolicyBest
	}
	return bankxp.PolicyFleet
}

// bankAllRate reads the ?rate= server experience multiplier (Engine-TS
// Environment.node.xpRate). It defaults to 1 and is clamped to something sane, because it is a
// number an operator types and a silly one would make every figure meaningless.
func bankAllRate(r *http.Request) float64 {
	v, err := strconv.ParseFloat(r.URL.Query().Get("rate"), 64)
	// NaN passes both range checks below and then fails the JSON encoding of every figure
	// it multiplies, a 500 for ?rate=NaN.
	if err != nil || math.IsNaN(v) || v <= 0 {
		return 1
	}
	if v > 100 {
		return 100
	}
	return v
}
