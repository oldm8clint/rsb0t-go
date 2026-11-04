package mastercraftplan

// The jewellery round's walking enchants (a user request: "ring enchanting should be done while walking instead
// of while standing at the furnace"). The decision is JewelleryView.WalkEnchant and NextJewelleryStep; the cast on the
// walk is scarabrunner.Travel's (scarabplan/walkenchant.go has the rule), the bookkeeping mastercrafter_jewellery.go's.

// KeyWalkEnchant turns the walking enchants on (the default) or off: off, the round enchants standing, at the
// furnace, as it did before 8 October 2026.
const KeyWalkEnchant = "craftWalkEnchant"

func init() { knownKeys[KeyWalkEnchant] = true }
