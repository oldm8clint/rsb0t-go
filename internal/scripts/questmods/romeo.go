package questmods

import (
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
)

// RomeoJuliet is Romeo & Juliet ready for the engine. It needs no executor of its own: its seven talks are generic steps
// (the walker climbs the stairs to Juliet's room through the graph's own edges), the message is drawn from the bank by
// the provisioner (Juliet issues no second one while the first is banked) and the cadava berry by the provisioner's
// ground rung at the three spawns south-east of Varrock, the potion being the Apothecary's to make.
func RomeoJuliet() questrunner.Quest { return questrunner.Quest{Module: questplan.RomeoJuliet} }
