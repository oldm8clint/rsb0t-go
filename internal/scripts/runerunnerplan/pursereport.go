package runerunnerplan

// What the purse did, in the log's words (a user request: "request mules entire cash stack and spend it on
// everything": the lines say what was asked, what was received and what was spent, so the owner can see it worked).
//
// Three lines carry it: the round's `purse round_need=...` line before the ask (mulePurse), the `purse FUNDED` or
// `purse SHORT` line after it, and the `purse round=... received=... spent=...` line when the round closes.

import "fmt"

// PurseFunded is the line after a top-up (or after a check that needed none) when bank plus pack holds the
// round's whole bill and the bank's reserve: what was held against it, what was asked and what came.
func PurseFunded(held, bill, reserve, asked, got int) string {
	target := bill + reserve
	return fmt.Sprintf("purse FUNDED: %d coins held (bank and pack) against a bill of %d and a reserve of %d, %d to spare; asked=%d received=%d; what the round does not spend stays in this account's bank",
		held, bill, reserve, held-target, asked, got)
}

// RoundPurse is one round's coin account: what came, what went and what stays.
type RoundPurse struct {
	Round int
	// Received is the coins the mule gave this round (the purse's top-up and the market's).
	Received int
	// Spent is every coin the round's stations paid shops, Ammo of it at the ammo stations.
	Spent, Ammo int
	// Bank is the last bank reading, Pack the coins carried when the round closed.
	Bank, Pack int
}

// String is the round-close line: nothing is handed back to the mule, so what is left is the next round's.
func (p RoundPurse) String() string {
	return fmt.Sprintf("purse round=%d received=%d spent=%d (runes_and_staffs=%d ammo=%d) kept=%d (bank %d + pack %d) for the next round; no coin goes back to the mule",
		p.Round, p.Received, p.Spent, p.Spent-p.Ammo, p.Ammo, p.Bank+p.Pack, p.Bank, p.Pack)
}
