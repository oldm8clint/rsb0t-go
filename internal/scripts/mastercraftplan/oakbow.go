package mastercraftplan

// MASTER STRINGS HIS OWN SHORTBOW FOR THE OAK ROUND (a user request). The two-tick oak chopper wields a plain Shortbow (no Ranged
// level). Until now master asked the mule for "a Shortbow" on every start, and the mule's name match gave the UNSTRUNG bow (obj 50;
// the strung one is obj 841, same name), so the chopper had a bow it could not wield. Now: a strung bow held or worn ends it; else
// the unstrung bow and a Bow string already in the pack are strung; else they are drawn from this account's own bank (1,150 strings
// there on 5 Nov); and only when the bank holds none does the ask go to the mule (which holds 190,850 strings), by the unstrung
// bow's qualified name (bowstringplan.AskName) so it gets the right obj.

// OakBowHeld is what the plan reads: counts of the Shortbow's unstrung and strung forms and of Bow strings, in the pack (and worn,
// for the strung bow), in this account's bank (BankKnown false: not read) and on the mule.
type OakBowHeld struct {
	StrungWorn, StrungPack   int
	UnstrungPack, StringPack int
	UnstrungBank, StringBank int
	BankKnown                bool
	UnstrungMule, StringMule int
}

// OakBowStep is what fetching the bow does next.
type OakBowStep string

// The steps.
const (
	// OakBowHave: a strung shortbow is worn or in the pack; nothing to do.
	OakBowHave OakBowStep = "have"
	// OakBowString: the pack holds an unstrung bow and a string; string them.
	OakBowString OakBowStep = "string"
	// OakBowBank: draw one unstrung bow and one string from this account's bank (what is missing from the pack), then string.
	OakBowBank OakBowStep = "bank"
	// OakBowAsk: ask the mule for what the pack and the bank lack, then string.
	OakBowAsk OakBowStep = "ask"
	// OakBowNone: nobody holds the parts.
	OakBowNone OakBowStep = "none"
)

// PlanOakBow chooses the step. It never plans to ask the mule for a bow while the account's own bank can supply the part.
func PlanOakBow(h OakBowHeld) OakBowStep {
	switch {
	case h.StrungWorn > 0 || h.StrungPack > 0:
		return OakBowHave
	case h.UnstrungPack > 0 && h.StringPack > 0:
		return OakBowString
	}
	needBow, needString := h.UnstrungPack == 0, h.StringPack == 0
	if h.BankKnown && (!needBow || h.UnstrungBank > 0) && (!needString || h.StringBank > 0) {
		return OakBowBank
	}
	if (!needBow || h.UnstrungBank > 0 || h.UnstrungMule > 0) && (!needString || h.StringBank > 0 || h.StringMule > 0) && (h.UnstrungMule > 0 || h.StringMule > 0) {
		return OakBowAsk
	}
	return OakBowNone
}

// OakBowAskLines is what the mule is asked for when the step is OakBowAsk: the unstrung bow (by its qualified name, askName) and a
// string, each only when neither the pack nor the bank holds it.
func OakBowAskLines(h OakBowHeld, askName, stringName string) []Line {
	var lines []Line
	if h.UnstrungPack == 0 && h.UnstrungBank == 0 && h.UnstrungMule > 0 {
		lines = append(lines, Line{Item: askName, Qty: 1})
	}
	if h.StringPack == 0 && h.StringBank == 0 && h.StringMule > 0 {
		lines = append(lines, Line{Item: stringName, Qty: 1})
	}
	return lines
}
