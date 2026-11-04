package bot

import (
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/trawlerplan"
)

// The crew's raw manta and turtle go to the mule, which cooks them and hands them out as food (trawlerplan.MuleFish): a crew
// member whose bank holds trawlerplan.MuleFishMin of them, at a bank visit, walks to the mule's post, draws them there as notes
// and trades them over (runerunner.Deliver, the consolidation trip's own trade). Only with the mule heard on this world, and at
// most every trawlerMuleFishEvery, so a mule that is away or a trade that fails costs one walk, not one a visit.
const trawlerMuleFishEvery = time.Hour

// muleFishBanked is how many trawlerplan.MuleFish the open bank holds.
func (b *fishingTrawlerBot) muleFishBanked(d *fleetrunner.Deps, bank *actions.Bank) int {
	n := 0
	for _, it := range bank.Items() {
		if o, ok := d.Config.Obj(int(it.ID)); ok && !o.Noted() && trawlerplan.IsMuleFish(o.Name) {
			n += int(it.Count)
		}
	}
	return n
}

// giveMuleFish walks the banked raw manta and turtle to the mule (see trawlerMuleFishEvery).
func (b *fishingTrawlerBot) giveMuleFish(ctx *Context, d *fleetrunner.Deps, banked int) {
	mule := b.settings.Mule
	if mule == "" || ctx.Bus == nil || time.Since(b.muleFishGiven) < trawlerMuleFishEvery {
		return
	}
	b.muleFishGiven = time.Now()
	b.say(ctx, "%d raw manta and turtle banked for %s; this build does not hand them over", banked, mule)
}
