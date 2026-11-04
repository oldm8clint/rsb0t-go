package wcfletchplan

// Fletching what the bank already holds — ALL of it, not one pack per visit.
//
// The fleet owner, 2026-09-23: "progressivewcfletch should check the bank for any logs prior to
// going and chopping more to fletch if the bank has logs, fletch them if not then chop". The
// first version drew ONE pack of the rung's logs on a bank visit and cut it, and that is enough
// on every rung but the first: a bow is one log to one unstackable item, so cutting a drawn pack
// leaves the pack full, the phase machine sends the account straight back to the bank, and the
// next pack is drawn on its own.
//
// Arrow shafts STACK. Cutting 26 plain logs leaves 390 shafts in ONE slot and 25 free slots, so
// no pack-full bank trip ever follows, and on 3dfb543a 26 rung-1 accounts walked away from the
// bank with 158-315 plain logs still in it after their single draw (20 of them made no bank trip
// at all in the 40-minute window). Those logs are the cheapest Fletching experience this script
// can reach: at the bank, with no walking and no chopping, content cuts one every two ticks
// (cut_logs.rs2's fletch_logs sets %action_delay to map_clock + 2 per log), about 2,400 an hour
// at a production tick — 18,000 Fletching xp an hour from shafts, against the 730 combined the
// same accounts were making at the saturated Seers camp.
//
// So on a stacking rung the account STAYS AT THE BANK until the bank is empty: it cuts what it
// drew there, draws again, and only then walks to its trees. Two conditions keep that from
// looping: the bank read is refreshed on every visit, and MaxBulkDrawFails visits in a row whose
// draw moved nothing end the run of draws.
//
// Not ONE failed draw: a random event interrupts a bank visit as easily as it interrupts a
// batch. Measured on the lab engine: a Strange plant spawned beside an account mid-sequence,
// the withdrawal came back "required trade screen is not open", and the first build of this rule
// took that single miss as "done" and walked away from 46 banked logs.
//
// It holds the account at the bank even while drawn logs are still in the pack, and that is not
// a tidiness. A make batch is interrupted by anything that opens a dialogue -- a level-up, most
// of all -- and a pack a few logs short of full is no longer "full", so the phase machine would
// otherwise send the account off to its patch carrying the rest of the draw, and it would only
// ever come back seventy tiles later for the next one. Measured on the lab engine, where the
// 10x experience rate levels Fletching every couple of logs: the first build of this rule cut 2
// of a 26-log draw at the bank and walked away with 24.

// MaxBulkDrawFails is how many bank visits in a row may fail to draw the rung's banked logs
// before the account gives up on them and goes to its trees. They are not lost: the next
// ordinary bank visit tries again.
const MaxBulkDrawFails = 3

// NearBankRadius is how close to the Seers bank an account must be for a draw to hold it there.
// A draw sequence broken off elsewhere -- a random event's teleport, say -- is not chased back
// across the map; it resumes on the next ordinary bank visit.
const NearBankRadius = 12

// BulkSnapshot is what BulkDrawDue reads.
type BulkSnapshot struct {
	// Fletching is true when this run is fletching and a knife is in the pack.
	Fletching bool
	// Stacking is true when the rung's product stacks (arrow shafts): a drawn pack, once cut, does
	// not fill the pack, so nothing else would ever send the account back for the next one.
	Stacking bool
	// BankedLogs is how many of THIS rung's logs the bank held after the last visit's draw.
	BankedLogs int
	// BankReadIsThisRung is true when that read was taken for the rung the account is on now. A
	// rung change makes the count stale: it counted a different species.
	BankReadIsThisRung bool
	// DrawFails is how many visits in a row have failed to draw logs the bank still showed.
	DrawFails int
	// NearBank is true when the account is within NearBankRadius of the Seers bank.
	NearBank bool
}

// BulkDrawDue reports whether the account should stay on the bank phase -- cutting what it drew,
// then drawing the rung's next banked logs -- rather than walk to its trees.
func BulkDrawDue(s BulkSnapshot) bool {
	return s.Fletching && s.Stacking && s.BankReadIsThisRung && s.BankedLogs > 0 && s.DrawFails < MaxBulkDrawFails && s.NearBank
}
