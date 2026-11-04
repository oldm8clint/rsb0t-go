package fleetplan

// The production budgets ("scarabprod", 2026-09-23): the defaults for the three settings the
// readiness critique showed a 100+ account cohort cannot supply at their old values. They are the
// DEFAULTS, so an account started with its settings left out (a dashboard click sends {}) gets
// numbers the fleet can actually feed; a lab fixture that wants a bigger bank still sets its own.
//
// Each one, with the arithmetic that retired its old value:
//
//   - stage3CastsTarget 500 -> 30. Stage 3 cannot COMPLETE until this many casts of the KQ spell
//     are banked (StageDone), and every Stage 6 restock refills to it. At Wind Bolt that was 500
//     chaos per account before Stage 5 -- 50,000 for a hundred accounts, against a mule holding
//     about 2,400 and Lundail regrowing 60 an hour for the whole world. Thirty is fifteen lives at
//     two casts a life: enough to leave Stage 3 and come back only every fifteen deaths.
//   - kqCastsPerLife 30 -> 2. Whatever a life carries is lost at its death, and at Hitpoints 20 her
//     randominc(31) hits allow about 1.3-2 accepted casts a life (the readiness study's range, lab
//     measured). Thirty carried was 28 chaos thrown on the floor every life -- about 15,800 chaos an
//     hour at 100 accounts, which only the market could pay for at roughly 1.5M coins an hour.
//   - kqRopeStock 10 -> 30. A restock tops the bank up to this, and Stage 6 only buys ropes on a
//     trip WEST, so ten sent an account back across two ships every five lives (two ropes a life).
//     Thirty is fifteen lives a trip. Ned in Draynor sells without limit on the respawn side
//     (nedrope.go), which makes the bank level matter less, not more.
const (
	ProdStage3CastsTarget = 30
	ProdKQCastsPerLife    = 2
	ProdKQRopeStock       = 30
)
