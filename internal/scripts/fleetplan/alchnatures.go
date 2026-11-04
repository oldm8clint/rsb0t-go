package fleetplan

// StockBudget is how many casts of alchemy stock a bank visit may leave in the pack (a deposit's keep) or draw into it
// (a kit draw), given the casts the visit's carry allows (budget) and the Nature runes the pack holds: all of the
// budget with at least one nature, none without. A budget of zero or less is zero.
func StockBudget(budget, natures int) int {
	if budget <= 0 || natures <= 0 {
		return 0
	}
	return budget
}

// CanCastStock is whether an account has Nature runes to cast alchemy stock with: some in the pack, some in its own
// bank (it draws them at its next bank visit), or the ask being made brings some (asked: the plan hands over at least
// one, PlanBringsNatures).
func CanCastStock(packNatures, bankNatures int, asked bool) bool {
	return packNatures > 0 || bankNatures > 0 || asked
}

// NoNaturesLine is the log line for a bank visit that leaves its alchemy stock banked for want of Nature runes: held
// casts of stock were in the pack (0 when it only drew), where names the visit.
func NoNaturesLine(where string, held int) string {
	if held > 0 {
		return "alch stock: carrying 0 of " + Itoa(held) + " (no Nature runes in the pack to cast it with, " + where + "): the stock stays banked"
	}
	return "alch stock: drawing none (no Nature runes in the pack or the bank to cast it with, " + where + "): the stock stays banked"
}
