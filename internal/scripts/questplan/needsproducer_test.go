package questplan

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/levels"
)

// THE NAILS ARE THE PRODUCER'S (plan 3.5 "Nails are a producer problem, not a per-account level"; decisions D4 and D13).
//
// Smithing 34 for 283 accounts is about 810 iron bars each (229k) against the 14k on the mule, so the planner must not send an
// ordinary account to the anvil for it: the account asks the mule for the nails and waits, and the one designated producer
// account plans the Smithing 34. ItemNeed.Producer is what says so (ProducerExclusive); the silver bar, whose Mining 20 and
// Smithing 20 an account may still train when the mule is out (plan 3.6), stays ProducerDesignated.

func nailsKey(qty int) string { return "item:" + strconv.Itoa(ObjNails) + ":" + strconv.Itoa(qty) }

// graduate is a Stage 5 graduate whose holdings were read, on the mule's world, with a mule holding iron bars and no nails.
func graduate() Subject {
	s := accountAt(map[string]int{"hitpoints": 30, "attack": 30, "strength": 30, "defence": 30, "magic": 30, "thieving": 25})
	s.Parked, s.Deferrals = map[string]time.Time{}, map[string]int{}
	s.OwnKnown, s.MuleKnown, s.World, s.MuleWorld = true, true, 1, 1
	s.MuleStock[ObjIronBar] = 14000
	return s
}

func planOwner(sub Subject, set NeedsSettings) NeedsPlan {
	return PlanNeeds(sub, OwnerLadder, DefaultNeedTable(), DefaultCatalogue(), FleetState{}, set, t0)
}

// An account that holds no nails is not sent to Smithing 34: Horror and Dragon Slayer wait on the nails, as a provision from
// the mule's producer and not as a gap, while the Smithing 20 that Elemental Workshop and Nature Spirit want is still trained.
func TestNailsAreNotAPerAccountStint(t *testing.T) {
	pl := planOwner(graduate(), DefaultNeedsSettings())

	if c := cand(pl, "stint:anvil-daggers:smithing:20"); c == nil || !c.Eligible {
		t.Errorf("the Smithing 20 stint is the legitimate one: %+v", c)
	} else {
		for _, want := range []string{ElementalWorkshopID, NatureSpiritID} {
			found := false
			for _, id := range c.Helps {
				found = found || id == want
			}
			if !found {
				t.Errorf("the Smithing 20 stint should help %s: %v", want, c.Helps)
			}
		}
		for _, id := range c.Helps {
			if id == HorrorID || id == DragonSlayerID {
				t.Errorf("the Smithing 20 stint does not serve the nails of %s: %v", id, c.Helps)
			}
		}
	}
	for _, c := range pl.Candidates {
		if c.Stint != nil && c.Stint.Skill == "smithing" && c.Stint.ToLevel > 20 {
			t.Errorf("an ordinary account is planned a Smithing stint past 20: %s", c.Key)
		}
	}
	for id, qty := range map[string]int{HorrorID: 8, DragonSlayerID: 12} {
		if _, ok := blockedNeed(pl, id, "skill:smithing:34"); ok {
			t.Errorf("%s: Smithing 34 is the producer's, not this account's need", id)
		}
		n, ok := blockedNeed(pl, id, nailsKey(qty))
		if !ok || n.Status != NeedProvision || !strings.Contains(n.Detail, "designated producer") || !strings.Contains(n.Detail, "ask the mule") {
			t.Errorf("%s: the nails are a provision from the producer: %+v %v", id, n, ok)
		}
	}
	for _, u := range pl.Unreachable {
		if u.Need.Kind == NeedItem && u.Need.ItemID == ObjNails {
			t.Errorf("waiting for the producer is not a gap: %+v", u)
		}
	}
}

// The designated producer is the account that trains Smithing 34, one stint serving both quests; once the mule holds the nails it
// trains nothing for them.
func TestTheDesignatedProducerPlansSmithing34ForTheNails(t *testing.T) {
	sub := graduate()
	sub.IsProducer = true
	pl := planOwner(sub, DefaultNeedsSettings())
	c := cand(pl, "stint:anvil-daggers:smithing:34")
	if c == nil || !c.Eligible || c.Stint == nil || c.Stint.Rung != "smithing34" {
		t.Fatalf("the producer's Smithing 34 stint: %+v", c)
	}
	helps := strings.Join(c.Helps, ",")
	if !strings.Contains(helps, HorrorID) || !strings.Contains(helps, DragonSlayerID) {
		t.Errorf("the stint serves Horror and Dragon Slayer: %v", c.Helps)
	}
	if _, ok := blockedNeed(pl, HorrorID, nailsKey(8)); ok {
		t.Error("the producer trains for the nails; it does not wait for them")
	}

	sub.MuleStock[ObjNails] = 500
	pl = planOwner(sub, DefaultNeedsSettings())
	if c := cand(pl, "stint:anvil-daggers:smithing:34"); c != nil {
		t.Errorf("a mule that holds the nails waives the producer's Smithing 34 too: %+v", c)
	}
}

// The mule answers an ordinary account's ask when it can be reached; a mule on another world is the far verdict, not a stint.
func TestNailsOnTheMuleAreAskedForNotTrained(t *testing.T) {
	near := graduate()
	near.MuleStock[ObjNails] = 500
	pl := planOwner(near, DefaultNeedsSettings())
	for id, qty := range map[string]int{HorrorID: 8, DragonSlayerID: 12} {
		if _, ok := blockedNeed(pl, id, "skill:smithing:34"); ok {
			t.Errorf("%s: Smithing 34 with the nails on the mule", id)
		}
		if _, ok := blockedNeed(pl, id, nailsKey(qty)); ok {
			t.Errorf("%s: the mule has the nails", id)
		}
	}

	far := graduate()
	far.World, far.MuleStock[ObjNails] = 3, 500
	pl = planOwner(far, DefaultNeedsSettings())
	if n, ok := blockedNeed(pl, HorrorID, nailsKey(8)); !ok || n.Status != NeedFar {
		t.Errorf("a mule on another world is far: %+v %v", n, ok)
	}
	if cand(pl, "stint:anvil-daggers:smithing:34") != nil {
		t.Error("a far mule is no reason to train Smithing 34")
	}
	set := DefaultNeedsSettings()
	set.MuleHop = true
	for _, producer := range []bool{false, true} {
		far.IsProducer = producer
		pl = planOwner(far, set)
		if _, ok := blockedNeed(pl, HorrorID, nailsKey(8)); ok {
			t.Errorf("producer %v: with the hop the mule's nails are reachable", producer)
		}
		if _, ok := blockedNeed(pl, HorrorID, "skill:smithing:34"); ok {
			t.Errorf("producer %v: nails the account can fetch waive Smithing 34", producer)
		}
	}
}

// The silver bar is not the nails: its skill link stays an account's fallback when the mule is out (plan 3.6), so Nature Spirit
// still sends an ordinary account to Mining 20 and Smithing 20.
func TestSilverKeepsItsPerAccountFallback(t *testing.T) {
	for _, in := range ItemNeeds {
		switch in.Item {
		case ObjSilverBar:
			if in.Producer != ProducerDesignated {
				t.Errorf("silver is %q: an account may still train for it", in.Producer)
			}
		case ObjNails:
			if in.Producer != ProducerExclusive {
				t.Errorf("nails are %q: only the producer trains for them", in.Producer)
			}
		}
	}
	sub := graduate()
	sub.Done[PriestInPerilID], sub.Done[RestlessGhostID] = true, true
	unmet, _ := UnmetOf(sub, NatureSpiritID)
	if !hasKey(unmet, "skill:smithing:20") || !hasKey(unmet, "skill:mining:20") {
		t.Errorf("silver with the mule out is trained for: %v", needKeys(unmet))
	}
}

// JudgeItem on an exclusive item: the producer trains, anyone else waits, and the mule and a missing executor are read as ever.
func TestJudgeItemWaitsForTheProducerOfAnExclusiveItem(t *testing.T) {
	set := DefaultNeedsSettings()
	in := ItemNeed{Item: 100, Name: "Thing", Qty: 2, Quests: []string{"q"}, Producer: ProducerExclusive,
		Chain: []ChainLink{
			{Kind: SourceBank}, {Kind: SourceMule},
			{Kind: SourceSkill, Skill: "smithing", Level: 34, Method: "make-thing", Built: true},
		}}
	sub := accountAt(map[string]int{"smithing": 10})
	sub.OwnKnown, sub.MuleKnown, sub.World, sub.MuleWorld = true, true, 1, 1
	j := JudgeItem(sub, set, in)
	if j.State != ItemProducer || !strings.Contains(j.Reason, "designated producer") || j.State.String() != "waiting for the designated producer" {
		t.Errorf("an ordinary account waits: %+v", j)
	}
	sub.MuleStock[100] = 1
	if j = JudgeItem(sub, set, in); j.State != ItemProducer || !strings.Contains(j.Reason, "holds 1 of the 2") {
		t.Errorf("a mule short of the ask does not open: %+v", j)
	}
	sub.IsProducer = true
	if j = JudgeItem(sub, set, in); j.State != ItemTrain || len(j.Skills) != 1 || j.Skills[0].Key() != "skill:smithing:34" {
		t.Errorf("the producer trains: %+v", j)
	}
	rich := accountAt(map[string]int{"smithing": 40})
	rich.OwnKnown, rich.IsProducer = true, true
	if j = JudgeItem(rich, set, in); j.State != ItemProvisioner {
		t.Errorf("a producer at the level makes it: %+v", j)
	}
	rich.IsProducer = false
	if j = JudgeItem(rich, set, in); j.State != ItemProducer {
		t.Errorf("being able to make it does not make an ordinary account the producer: %+v", j)
	}
	// The mule holds it: any account asks. On another world with no hop it is far, and far outranks waiting.
	sub.IsProducer = false
	sub.MuleStock[100] = 5
	if j = JudgeItem(sub, set, in); j.State != ItemMuleHas {
		t.Errorf("the mule answers: %+v", j)
	}
	sub.World = 3
	if j = JudgeItem(sub, set, in); j.State != ItemMuleFar {
		t.Errorf("a far mule: %+v", j)
	}
	// An executor that is not built cannot be waited for: the verdict is the gap, not the producer.
	broken := in
	broken.Chain = []ChainLink{{Kind: SourceBank}, {Kind: SourceMule}, {Kind: SourceSkill, Skill: "smithing", Level: 34, Method: "make-thing", Built: false}}
	sub.World, sub.MuleStock[100] = 1, 0
	if j = JudgeItem(sub, set, broken); j.State != ItemStuck {
		t.Errorf("no executor, no producer: %+v", j)
	}
}

// The table keeps its promises: an exclusive item is one somebody can make and an account can ask for, and every item gate that
// the exclusive rule skips states the quantity of the item that replaces it.
func TestExclusiveItemsAreMadeByAProducerAndAskedForByAnAccount(t *testing.T) {
	tab := DefaultNeedTable()
	exclusive := 0
	for _, in := range ItemNeeds {
		if in.Producer != ProducerExclusive {
			continue
		}
		exclusive++
		mule, skill := false, false
		for _, l := range in.Chain {
			mule = mule || l.Kind == SourceMule
			skill = skill || (l.Kind == SourceSkill && l.Built)
		}
		if !mule || !skill {
			t.Errorf("%s is exclusive to the producer: it needs a mule link to ask and a built skill link to make it (mule %v skill %v)", in.Name, mule, skill)
		}
		if in.Gap != "" {
			t.Errorf("%s names a gap (%s): nobody could make it", in.Name, in.Gap)
		}
		for _, q := range in.Quests {
			gates := 0
			for _, n := range tab.needs[q] {
				if g, ok := tab.producerGate(q, n); ok && g.Item == in.Item {
					gates++
					if n.WaiverQty != in.Qty {
						t.Errorf("%s: the gate is waived by %d of %s and the item need asks for %d", q, n.WaiverQty, in.Name, in.Qty)
					}
				}
			}
			if gates != 1 {
				t.Errorf("%s: %d skill gates are skipped for %s's producer, want the one level that makes it", q, gates, in.Name)
			}
		}
	}
	if exclusive == 0 {
		t.Error("the nails are exclusive to the producer")
	}
}

// The fleet report agrees with the planner: an ordinary account is billed Smithing 20 and not 34, the producer 34, and Horror counts
// the nails for the one and the Smithing 34 for the other.
func TestTheReportBillsOnlyTheProducerForSmithing34(t *testing.T) {
	ordinary, producer := graduate(), graduate()
	producer.IsProducer = true
	rep := FleetNeeds([]Subject{ordinary, producer}, OwnerLadder, DefaultNeedTable(), DefaultCatalogue(), DefaultNeedsSettings(), false)
	var bill *SkillBill
	for i := range rep.Bill {
		if rep.Bill[i].Skill == "smithing" {
			bill = &rep.Bill[i]
		}
	}
	if bill == nil || bill.Accounts != 2 || bill.MissingXP != int64(levels.XPAtLevel(20)+levels.XPAtLevel(34)) {
		t.Fatalf("smithing bill: %+v (want both accounts, %d + %d xp)", bill, levels.XPAtLevel(20), levels.XPAtLevel(34))
	}
	for _, q := range rep.Quests {
		if q.Quest != HorrorID {
			continue
		}
		count := map[string]NeedCount{}
		for _, c := range q.Hard {
			count[c.Need.Key()] = c
		}
		if c := count["skill:smithing:34"]; c.Accounts != 1 {
			t.Errorf("Horror's Smithing 34 is the producer's alone: %+v", c)
		}
		c := count[nailsKey(8)]
		if c.Accounts != 1 || c.Status != NeedProvision || !strings.Contains(c.Acquirer, "designated producer") {
			t.Errorf("Horror's nails are the ordinary account's, from the producer's stock: %+v", c)
		}
		return
	}
	t.Error("no Horror in the report")
}
