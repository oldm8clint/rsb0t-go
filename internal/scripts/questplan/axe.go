package questplan

// The axe every woodcutting step needs, found once: any axe the account already owns before an iron axe
// is bought. Two kits had it wrong in opposite ways. Olaf's kit asked for an iron axe by id with no source
// but the bank and the mule, so an account whose bank held a steel axe, or any axe but an iron one, was
// "blocked" (13 of 18 failed Olaf's kit rounds on 14 Oct 2026: "1 Iron axe: the mule publishes none; no
// ground, skill or shop source listed"; the mule holds no iron axe, 17 steel, 19 bronze and 17 rune). The
// Woodcutting rung read the bank for an axe but the shop only for an iron one.

// Axe object ids (Axes lists them weakest first).
const (
	ObjBronzeAxe  = 1351
	ObjSteelAxe   = 1353
	ObjMithrilAxe = 1355
	ObjAdamantAxe = 1357
	ObjRuneAxe    = 1359
)

// MethodChopOak is the provisioner's skill source that fells oaks for oak logs (MethodChopLogs's, for oaks).
const MethodChopOak = "chop-oak"

// AxeLevel is the Woodcutting level each axe in Axes needs to chop with it (the content's
// woodcutting_axe_checker refuses an axe above the level: "You do not have an axe that you have the
// Woodcutting level to use").
var AxeLevel = map[int]int{
	ObjBronzeAxe: 1, ObjIronAxe: 1, ObjSteelAxe: 6, ObjMithrilAxe: 21, ObjAdamantAxe: 31, ObjRuneAxe: 41,
}

// AxeHeld reports any axe in the pack or worn.
func AxeHeld(s Snapshot) bool {
	for _, a := range Axes {
		if s.InvIDs[a] > 0 || s.WornIDs[a] > 0 {
			return true
		}
	}
	return false
}

// AxeLine is the kit line for an axe to chop with, for a pack that holds none: the best axe the bank
// holds that the Woodcutting level can use (a bank not read yet is not an empty one: the caller scans
// it first), else an iron axe, which the provisioner takes from the bank or the mule if either has one
// and buys from Aemad's shop (56 gp) otherwise. The line counts a worn axe as held. A Woodcutting level
// the snapshot does not know (0) judges no axe too high.
func AxeLine(s Snapshot) KitLine {
	level := s.Level("woodcutting")
	if s.BankKnown {
		for i := len(Axes) - 1; i >= 0; i-- {
			a := Axes[i]
			if s.BankIDs[a] > 0 && (level <= 0 || AxeLevel[a] <= level) {
				return KitLine{ID: a, Name: "Axe", Qty: 1, Worn: true}
			}
		}
	}
	return KitLine{ID: ObjIronAxe, Name: "Iron axe", Qty: 1, Worn: true,
		Sources: []Source{{Kind: SourceShop, Shop: AemadShop, Price: 56}}}
}
