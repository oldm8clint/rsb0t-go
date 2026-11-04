package questmods

// Tai Bwo Wannai Trio through the real engine (Engine.Run) on a simulated session, in the manner of the other quests' engine tests: the
// module's own Decide runs against snapshots built from a fake pack, the quest tab's colour, the position and the journal state, and
// every step it asks for is performed by a stub that changes that world the way the content does (tbwt_lubufu.rs2, tbwt_tiadeche.rs2,
// tbwt_tamayu.rs2, tbwt_tinsay.rs2 and the item scripts). What is proven is the state machine under the engine's bookkeeping: the order
// of the legs, the items each hand-in takes, a healthy run never parked, a burnt Karambwan recovered from, and a quest short of its
// scarce supplies parked before the account has crossed the sea. Each custom's own live execution is the real-data tests' and the logs'.

import (
	"slices"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/quests"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
)

// tbRig is the content's six varps and the simulated account they belong to.
type tbRig struct {
	r *csRig
	// main, lubufu, tiadeche, tinsay and tamayu are %tbwt_main and the four brothers' varps; doses is Tamayu's agility count, and
	// acceptable and kp the spear bits of %tbwt_flags.
	main, lubufu, tiadeche, tinsay, tamayu int
	doses                                  int
	acceptable, kp                         bool
	// delivered is the Karambwanji Lubufu holds.
	delivered int
	// burn makes the next raw Karambwan cooked burn.
	burn bool
	// steps counts the executors that ran, by custom name.
	ran map[string]int
}

// newTbCSRig is newCSRig with the runes stackable as they are in the game (the quest's pack holds a dozen kinds of item).
func newTbCSRig(t *testing.T, at tile.Tile) *csRig {
	t.Helper()
	sim := actionsim.New(actionsim.Objs{Stackable: func(id int32) bool {
		switch int(id) {
		case questplan.CoinsID, questplan.ObjFireRune, questplan.ObjAirRune, questplan.ObjChaosRune, questplan.TbObjNatureRune:
			return true
		}
		return false
	}}, nil)
	sim.StandAt(at.X, at.Z)
	r := &csRig{t: t, sim: sim}
	d := &questrunner.Deps{
		Session: sim, Components: actionsim.Components(),
		Log:   func(s string) { r.log = append(r.log, s) },
		Delay: func(time.Duration) bool { time.Sleep(time.Millisecond); return true },
	}
	prov := &questrunner.Provisioner{Bank: &questrunner.BankView{Counts: map[int]int{}, Known: true, At: time.Now().Add(time.Hour)}}
	r.e = &questrunner.Engine{D: d, Prov: prov, Store: questplan.NewStore()}
	return r
}

func newTbRig(t *testing.T, at tile.Tile) *tbRig {
	w := &tbRig{r: newTbCSRig(t, at), ran: map[string]int{}}
	w.r.setTab(questplan.TaiBwoName, quests.NotStarted)
	w.sync()
	return w
}

// state is the journal's reading of the varps, by the rules the journal's text follows (taibwo_journal_test.go checks them against
// the script in the questplan package).
func (w *tbRig) state() questplan.TbwtState {
	st := questplan.TbwtState{Phase: questplan.TbPhaseStarted}
	if w.main < 3 {
		st.Phase = questplan.TbPhasePre
		return st
	}
	if w.main == 4 {
		st.Phase = questplan.TbPhaseBrothers
	}
	switch {
	case w.lubufu <= 1:
		st.Lubufu = questplan.TbLubufuNone
	case w.lubufu <= 3:
		st.Lubufu = questplan.TbLubufuMet
	case w.lubufu == 4:
		st.Lubufu = questplan.TbLubufuOffered
	case w.lubufu < 25:
		st.Lubufu = questplan.TbLubufuBait
	case w.lubufu < 29:
		st.Lubufu = questplan.TbLubufuGiven
	case w.lubufu == 29:
		st.Lubufu = questplan.TbLubufuApprentice
	default:
		st.Lubufu = questplan.TbLubufuDone
	}
	st.Tiadeche = min(w.tiadeche, questplan.TbTiadecheDone)
	if st.Tiadeche >= questplan.TbTiadecheManual {
		st.Lubufu = questplan.TbLubufuDone
	}
	st.Tinsay, st.Tamayu = w.tinsay, w.tamayu
	if w.tamayu == questplan.TbTamayuWatched {
		st.Agility, st.Spear = w.doses >= 4, w.acceptable && w.kp
	}
	if w.tamayu >= questplan.TbTamayuComplete {
		st.Agility, st.Spear = true, true
	}
	return st
}

// sync tells the rig's snapshot what the journal would say.
func (w *tbRig) sync() {
	w.r.stage, w.r.stageKnown = w.state().Stage(), true
}

func (w *tbRig) stand(at tile.Tile) { w.r.sim.StandAt(at.X, at.Z) }

func (w *tbRig) held(id int) int { return w.r.held(id) }

// spearBits is what Tamayu's script makes of a spear he is handed.
func (w *tbRig) handSpear(id int) {
	w.acceptable, w.kp = false, false
	for _, sp := range questplan.TbSpears {
		if id == sp.ID {
			w.acceptable = true
		}
		if id == sp.PoisonID {
			w.acceptable, w.kp = true, true
		}
	}
}

// apply plays the content's side of every step. answers override a step by its key.
func (w *tbRig) apply(answers map[string]func(step questplan.Step) (bool, bool)) func(step questplan.Step) bool {
	r := w.r
	return func(step questplan.Step) bool {
		defer w.sync()
		key := step.Custom
		switch step.Kind {
		case questplan.StepProvision:
			key = "provision:" + step.KitName
		case questplan.StepDeposit:
			key = "deposit"
		case questplan.StepOpHeld:
			key = "drop"
		}
		w.ran[key]++
		if f, ok := answers[key]; ok {
			if res, handled := f(step); handled {
				return res
			}
		}
		switch key {
		case "provision:" + questplan.KitTbScarce:
			r.give(3032, 1)
			r.give(1243, 1)
			r.give(questplan.TbObjNatureRune, 1)
		case "provision:" + questplan.KitTbTools:
			r.give(questplan.CoinsID, 300)
			r.give(questplan.TbObjNet, 1)
			r.give(questplan.TbObjPestle, 1)
			r.give(questplan.ObjKnife, 1)
			r.give(questplan.ObjSeaweed, 1)
			r.give(questplan.ObjFireRune, 4)
			r.give(questplan.ObjAirRune, 12)
			r.give(questplan.ObjChaosRune, 6)
			r.give(questplan.ObjShark, 6)
		case "drop":
			r.take(step.ItemID, 1)
		case questplan.CustomTbStart:
			w.stand(questplan.TbTimfrakuAnchor)
			w.main = 3
			r.setTab(questplan.TaiBwoName, quests.InProgress)
		case questplan.CustomTbLubufuMeet:
			w.stand(questplan.TbLubufuAnchor)
			w.lubufu = 5
		case questplan.CustomTbFish:
			w.stand(questplan.TbLakeShoals[0])
			for r.held(questplan.TbObjRawKarambwanji) < step.Qty {
				r.give(questplan.TbObjRawKarambwanji, 1)
			}
		case questplan.CustomTbLubufuGive:
			w.stand(questplan.TbLubufuAnchor)
			n := min(r.held(questplan.TbObjRawKarambwanji), 20-w.delivered)
			r.take(questplan.TbObjRawKarambwanji, n)
			w.delivered += n
			w.lubufu = 5 + w.delivered
			if w.delivered >= 20 {
				w.lubufu = 25
			}
		case questplan.CustomTbLubufuAsk:
			w.stand(questplan.TbLubufuAnchor)
			w.lubufu = 31
			r.give(questplan.TbObjVessel, 1)
		case questplan.CustomTbVessel:
			w.stand(questplan.TbLubufuAnchor)
			r.give(questplan.TbObjVessel, 1)
		case questplan.CustomTbBait:
			r.take(questplan.TbObjRawKarambwanji, 1)
			r.take(questplan.TbObjVessel, 1)
			r.give(questplan.TbObjVesselLoaded, 1)
		case questplan.CustomTbTiadecheMeet:
			w.stand(questplan.TbTiadecheAnchor)
			switch {
			case w.tiadeche < 2:
				w.tiadeche = 2
			case w.tiadeche == 3:
				w.tiadeche = 4
			case w.tiadeche == 5:
				// The manual is not in the pack: the page cannot say it is gone.
				if r.held(questplan.TbObjManual) > 0 {
					r.take(questplan.TbObjManual, 1)
					w.tiadeche = 6
					w.maybeBrothers()
				}
			}
		case questplan.CustomTbTiadecheCatch:
			w.stand(questplan.TbTiadecheAnchor)
			r.take(questplan.TbObjVesselLoaded, 1)
			r.give(questplan.TbObjRawKarambwan, 1)
			w.tiadeche = 4
		case questplan.CustomTbTiadecheGift:
			w.stand(questplan.TbTiadecheAnchor)
			return false
		case questplan.CustomTbTamayuMeet:
			w.stand(questplan.TbTamayuAnchor)
			w.tamayu = 2
		case questplan.CustomTbTamayuHunt:
			w.stand(questplan.TbTamayuAnchor)
			if step.Qty == 0 {
				w.tamayu = 3
			} else if w.doses >= 4 && w.acceptable && w.kp {
				w.tamayu = 4
			} else {
				return false
			}
		case questplan.CustomTbTamayuGive:
			w.stand(questplan.TbTamayuAnchor)
			id := step.Qty
			for _, p := range questplan.TbPotions {
				if id == p.ID {
					r.take(id, 1)
					w.doses = min(4, w.doses+p.Doses)
					return true
				}
			}
			r.take(id, 1)
			w.handSpear(id)
		case questplan.CustomTbCook:
			w.stand(questplan.TbFire)
			switch step.Qty {
			case questplan.TbObjRawKarambwan:
				r.take(questplan.TbObjRawKarambwan, 1)
				if w.burn {
					w.burn = false
					r.give(questplan.TbObjBurntKarambwan, 1)
				} else {
					r.give(questplan.TbObjPoorKarambwan, 1)
				}
			case questplan.TbObjBurntBonesInPaste:
				r.take(questplan.TbObjBurntBonesInPaste, 1)
				r.give(questplan.TbObjMarinatedBones, 1)
			}
		case questplan.CustomTbGrind:
			r.take(step.Qty, 1)
			if step.Qty == questplan.TbObjPoorKarambwan {
				r.give(questplan.TbObjPoisonPaste, 1)
			} else {
				r.give(questplan.TbObjKarambwanjiPaste, 1)
			}
		case questplan.CustomTbPoison:
			for _, sp := range questplan.TbSpears {
				if sp.ID == step.Qty {
					r.take(sp.ID, 1)
					r.take(questplan.TbObjPoisonPaste, 1)
					r.give(sp.PoisonID, 1)
				}
			}
		case questplan.CustomTbTinsayMeet:
			w.stand(questplan.TbTinsayAnchor)
			switch w.tinsay {
			case 0, 1:
				w.tinsay = 2
			case 3:
				w.tinsay = 4
			case 5:
				w.tinsay = 6
			}
		case questplan.CustomTbBanana:
			w.stand(questplan.BananaGrove)
			r.give(questplan.ObjBanana, 1)
		case questplan.CustomTbSlice:
			r.take(questplan.ObjBanana, 1)
			r.give(questplan.TbObjSlicedBanana, 1)
		case questplan.CustomTbRum:
			w.stand(questplan.ZamboShop.Anchor)
			r.give(questplan.ObjKaramjaRum, 1)
		case questplan.CustomTbMix:
			r.take(questplan.TbObjSlicedBanana, 1)
			r.take(questplan.ObjKaramjaRum, 1)
			r.give(questplan.TbObjRumSlicedBanana, 1)
		case questplan.CustomTbMonkey:
			w.stand(questplan.TbMonkeys)
			r.give(questplan.TbObjMonkeyCorpse, 1)
		case questplan.CustomTbSkin:
			w.stand(questplan.TbTamayuAnchor)
			if w.tamayu < 4 {
				return false
			}
			r.take(questplan.TbObjMonkeyCorpse, 1)
			r.give(questplan.TbObjMonkeySkin, 1)
		case questplan.CustomTbSandwich:
			r.take(questplan.TbObjMonkeySkin, 1)
			r.take(questplan.ObjSeaweed, 1)
			r.give(questplan.TbObjSandwich, 1)
		case questplan.CustomTbJogre:
			w.stand(questplan.TbJogreStage)
			r.give(questplan.TbObjJogreBones, 1)
		case questplan.CustomTbBurn:
			r.take(questplan.TbObjJogreBones, 1)
			r.take(questplan.TbObjNatureRune, 1)
			r.take(questplan.ObjFireRune, 4)
			r.give(questplan.TbObjBurntJogreBones, 1)
		case questplan.CustomTbPasteBones:
			r.take(questplan.TbObjBurntJogreBones, 1)
			r.take(questplan.TbObjKarambwanjiPaste, 1)
			r.give(questplan.TbObjBurntBonesInPaste, 1)
		case questplan.CustomTbTinsayGive:
			w.stand(questplan.TbTinsayAnchor)
			switch step.Qty {
			case questplan.TbObjRumSlicedBanana:
				if w.tinsay != 2 {
					return false
				}
				r.take(step.Qty, 1)
				w.tinsay = 4
			case questplan.TbObjSandwich:
				if w.tinsay != 4 {
					return false
				}
				r.take(step.Qty, 1)
				w.tinsay = 6
			case questplan.TbObjMarinatedBones:
				if w.tinsay != 6 {
					return false
				}
				r.take(step.Qty, 1)
				w.tinsay = 7
			case questplan.TbObjVessel, questplan.TbObjVesselLoaded:
				if w.tinsay != 7 || w.tiadeche != 4 {
					return false
				}
				r.take(step.Qty, 1)
				r.give(questplan.TbObjManual, 1)
				w.tiadeche = 5
			default:
				r.t.Errorf("Tinsay was handed obj %d", step.Qty)
				return false
			}
		case questplan.CustomTbManual:
			w.stand(questplan.TbTiadecheAnchor)
			if w.tiadeche != 5 || r.held(questplan.TbObjManual) == 0 {
				return false
			}
			r.take(questplan.TbObjManual, 1)
			w.tiadeche = 6
			w.maybeBrothers()
		case questplan.CustomTbReport:
			w.stand(questplan.TbTimfrakuAnchor)
			if w.main != 4 {
				return false
			}
			w.main = 6
			r.setTab(questplan.TaiBwoName, quests.Complete)
		default:
			r.t.Errorf("the scenario has no answer for the step %q (%s)", key, step.Describe())
			return false
		}
		return true
	}
}

// maybeBrothers is tbwt_get_brother_completion: when the last brother is complete, %tbwt_main moves to 4.
func (w *tbRig) maybeBrothers() {
	if w.tinsay == 7 && w.tamayu == 4 && w.tiadeche == 6 && w.lubufu >= 31 && w.main == 3 {
		w.main = 4
	}
}

// The whole quest, every executor a stub, from an account on the mainland with an empty pack.
func TestTaiBwoTrioReachesCompleteThroughTheEngine(t *testing.T) {
	w := newTbRig(t, questplan.ArdougneSouthBank)
	res := w.r.run(TaiBwoTrio(), w.apply(nil))
	if res.Outcome != questrunner.OutcomeComplete {
		t.Fatalf("the run ended %s (%s): %v", res.Outcome, res.Reason, w.r.log)
	}
	got := w.r.described()
	// The order of the legs: the supplies, the job, Lubufu (the meeting, the bait, the questions), Tiadeche's catch, Tamayu (the vow, the
	// first hunt, the potion, the spear's chain, the killing hunt), Tinsay (the news, the rum, the sandwich, the marinade), the second vessel,
	// the manual and the report.
	order := []string{"provision tbwt-scarce", "provision tbwt-tools", questplan.CustomTbStart, questplan.CustomTbLubufuMeet, questplan.CustomTbFish, questplan.CustomTbLubufuGive,
		questplan.CustomTbLubufuAsk, questplan.CustomTbBait, questplan.CustomTbTiadecheCatch, questplan.CustomTbTamayuMeet, questplan.CustomTbTamayuHunt,
		questplan.CustomTbTamayuGive, questplan.CustomTbCook, questplan.CustomTbGrind, questplan.CustomTbPoison, questplan.CustomTbTamayuGive, questplan.CustomTbTamayuHunt,
		questplan.CustomTbTinsayMeet, questplan.CustomTbBanana, questplan.CustomTbSlice, questplan.CustomTbRum, questplan.CustomTbMix, questplan.CustomTbTinsayGive,
		questplan.CustomTbMonkey, questplan.CustomTbSkin, questplan.CustomTbSandwich, questplan.CustomTbTinsayGive,
		questplan.CustomTbJogre, questplan.CustomTbBurn, questplan.CustomTbGrind, questplan.CustomTbPasteBones, questplan.CustomTbCook, questplan.CustomTbTinsayGive,
		questplan.CustomTbVessel, questplan.CustomTbTinsayGive, questplan.CustomTbManual, questplan.CustomTbReport}
	at := 0
	for _, want := range order {
		found := false
		for ; at < len(got); at++ {
			if strings.Contains(got[at], want) {
				found = true
				at++
				break
			}
		}
		if !found {
			t.Fatalf("the step %q does not follow the ones before it in\n  %s", want, strings.Join(got, "\n  "))
		}
	}
	if rec := w.r.e.Store.Record(questplan.TaiBwoID).Completions; len(rec) != 1 || !rec[0].Earned() {
		t.Errorf("completions %+v, want one earned", rec)
	}
	// The pack ends with the tools and the food the quest never used up, and none of the quest's own items.
	for _, id := range []int{questplan.TbObjVessel, questplan.TbObjVesselLoaded, questplan.TbObjManual, questplan.TbObjRawKarambwan, questplan.TbObjPoorKarambwan,
		questplan.TbObjPoisonPaste, questplan.TbObjJogreBones, questplan.TbObjMarinatedBones, questplan.ObjKaramjaRum, questplan.ObjBanana} {
		if n := w.held(id); n != 0 {
			t.Errorf("obj %d is still held (%d) after the quest", id, n)
		}
	}
	// The supplies were fetched once each, and the Karambwanji beyond the three kept were dropped (the lake gave more than the quest uses).
	if w.ran["provision:"+questplan.KitTbScarce] != 1 || w.ran["provision:"+questplan.KitTbTools] != 1 {
		t.Errorf("the supplies passes ran %d and %d times", w.ran["provision:"+questplan.KitTbScarce], w.ran["provision:"+questplan.KitTbTools])
	}
	t.Logf("%d steps: %s", len(got), strings.Join(got, " | "))
}

// The first raw Karambwan burns: the burnt one is dropped, Tiadeche's second gift is asked for (and refused: his first "Yes" set the
// bit), and the vessel brings another up at Fishing 65; the quest goes on to its end. An account under Fishing 65 parks instead, with the
// reason, and the park is a supply park.
func TestTaiBwoTrioABurntKarambwanIsReplacedByTheVessel(t *testing.T) {
	for _, fishing := range []int{70, 60} {
		w := newTbRig(t, questplan.ArdougneSouthBank)
		w.burn = true
		tfSeed(w.r, "fishing", fishing)
		res := w.r.run(TaiBwoTrio(), w.apply(map[string]func(questplan.Step) (bool, bool){
			questplan.CustomTbKarambwan: func(step questplan.Step) (bool, bool) {
				w.stand(tile.Tile{X: 2768, Z: 3165})
				w.r.take(questplan.TbObjRawKarambwanji, 1)
				w.r.give(questplan.TbObjRawKarambwan, 1)
				return true, true
			},
		}))
		if w.ran[questplan.CustomTbTiadecheGift] != 1 || w.ran["drop"] < 1 {
			t.Errorf("Fishing %d: the second gift was asked for %d times and %d drops ran", fishing, w.ran[questplan.CustomTbTiadecheGift], w.ran["drop"])
		}
		if fishing >= questplan.TbMinFishingForKarambwan {
			if res.Outcome != questrunner.OutcomeComplete || w.ran[questplan.CustomTbKarambwan] != 1 {
				t.Errorf("Fishing %d: the run ended %s (%s) after %d vessel casts: %v", fishing, res.Outcome, res.Reason, w.ran[questplan.CustomTbKarambwan], w.r.log)
			}
			continue
		}
		if res.Outcome != questrunner.OutcomeParked || !strings.Contains(res.Reason, "Fishing") || w.ran[questplan.CustomTbKarambwan] != 0 {
			t.Errorf("Fishing %d: the run ended %s (%s)", fishing, res.Outcome, res.Reason)
		}
		if parked, _ := w.r.e.Store.Parked(questplan.TaiBwoID, time.Now().Add(5*time.Hour)); !parked {
			t.Errorf("Fishing %d: not parked for hours", fishing)
		}
	}
}

// With no agility potion and no spear anywhere, the quest parks on the mainland with the reason, before any tool is carried or the
// sea crossed.
func TestTaiBwoTrioParksBeforeTheCrossingWhenTheScarceSuppliesAreNowhere(t *testing.T) {
	w := newTbRig(t, questplan.ArdougneSouthBank)
	res := w.r.run(TaiBwoTrio(), w.apply(map[string]func(questplan.Step) (bool, bool){
		"provision:" + questplan.KitTbScarce: func(questplan.Step) (bool, bool) {
			w.r.give(questplan.TbObjNatureRune, 1)
			return false, true // the pass ends with the potions and the spear unsourced
		},
	}))
	if res.Outcome != questrunner.OutcomeParked || !strings.Contains(res.Reason, "agility") || !strings.Contains(res.Reason, "spear") {
		t.Fatalf("the run ended %s (%s): %v", res.Outcome, res.Reason, w.r.log)
	}
	if w.ran["provision:"+questplan.KitTbTools] != 0 || w.ran[questplan.CustomTbStart] != 0 {
		t.Errorf("tools were fetched (%d) or the job taken (%d) by an account that cannot finish", w.ran["provision:"+questplan.KitTbTools], w.ran[questplan.CustomTbStart])
	}
	if parked, _ := w.r.e.Store.Parked(questplan.TaiBwoID, time.Now().Add(5*time.Hour)); !parked {
		t.Error("not parked for hours")
	}
	if parked, _ := w.r.e.Store.Parked(questplan.TaiBwoID, time.Now().Add(7*time.Hour)); parked {
		t.Error("parked past the six hours of a supply park")
	}
}

// A leg that cannot be done parks the quest on its Decide's wait, once, with its name; the leg is run once.
func TestTaiBwoTrioALegThatFailsParksTheQuest(t *testing.T) {
	for _, custom := range []string{questplan.CustomTbLubufuMeet, questplan.CustomTbFish, questplan.CustomTbStart} {
		w := newTbRig(t, questplan.ArdougneSouthBank)
		res := w.r.run(TaiBwoTrio(), w.apply(map[string]func(questplan.Step) (bool, bool){
			custom: func(questplan.Step) (bool, bool) { return false, true },
		}))
		if res.Outcome != questrunner.OutcomeParked || !strings.Contains(res.Reason, custom) || w.ran[custom] != 1 {
			t.Errorf("%s: outcome %s after %d run(s): %q", custom, res.Outcome, w.ran[custom], res.Reason)
		}
		if parked, _ := w.r.e.Store.Parked(questplan.TaiBwoID, time.Now().Add(29*time.Minute)); !parked {
			t.Errorf("%s: not parked for the half hour", custom)
		}
		if parked, _ := w.r.e.Store.Parked(questplan.TaiBwoID, time.Now().Add(31*time.Minute)); parked {
			t.Errorf("%s: parked past the half hour", custom)
		}
	}
}

// One death anywhere parks the quest for a day (MaxDeaths 1): the pack is gone and the price was wrong for this account.
func TestTaiBwoTrioOneDeathParksItForADay(t *testing.T) {
	w := newTbRig(t, questplan.ArdougneSouthBank)
	w.r.e.D.Chat = chatlog.New()
	res := w.r.run(TaiBwoTrio(), w.apply(map[string]func(questplan.Step) (bool, bool){
		questplan.CustomTbJogre: func(questplan.Step) (bool, bool) {
			w.r.e.D.Chat.Record("Oh dear, you are dead!")
			return false, true
		},
	}))
	if res.Outcome != questrunner.OutcomeParked || !strings.Contains(res.Reason, "the quest's cap is 1") {
		t.Fatalf("outcome %s: %q (%v)", res.Outcome, res.Reason, w.r.log)
	}
	if parked, _ := w.r.e.Store.Parked(questplan.TaiBwoID, time.Now().Add(23*time.Hour)); !parked {
		t.Error("not parked for the day")
	}
}

// The module declares every custom its Decide can ask for, and the engine can run it: a custom the quest names has an executor.
func TestTaiBwoTrioDeclaresAnExecutorForEveryCustomItAsksFor(t *testing.T) {
	q := TaiBwoTrio()
	for _, name := range []string{questplan.CustomTbStart, questplan.CustomTbReport, questplan.CustomTbFish, questplan.CustomTbLubufuMeet, questplan.CustomTbLubufuGive,
		questplan.CustomTbLubufuAsk, questplan.CustomTbVessel, questplan.CustomTbBait, questplan.CustomTbTiadecheMeet, questplan.CustomTbTiadecheCatch,
		questplan.CustomTbTiadecheGift, questplan.CustomTbKarambwan, questplan.CustomTbTamayuMeet, questplan.CustomTbTamayuHunt, questplan.CustomTbTamayuGive,
		questplan.CustomTbCook, questplan.CustomTbGrind, questplan.CustomTbPoison, questplan.CustomTbRum, questplan.CustomTbBanana, questplan.CustomTbSlice,
		questplan.CustomTbMix, questplan.CustomTbMonkey, questplan.CustomTbSkin, questplan.CustomTbSandwich, questplan.CustomTbJogre, questplan.CustomTbBurn,
		questplan.CustomTbPasteBones, questplan.CustomTbTinsayMeet, questplan.CustomTbTinsayGive, questplan.CustomTbManual} {
		if q.Customs[name] == nil {
			t.Errorf("no executor for %q", name)
		}
	}
	if len(q.Customs) != 31 {
		names := make([]string, 0, len(q.Customs))
		for n := range q.Customs {
			names = append(names, n)
		}
		slices.Sort(names)
		t.Errorf("%d executors: %v", len(q.Customs), names)
	}
}
