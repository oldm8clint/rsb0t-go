package questplan

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The journal parser against the journal the content renders. tbJournal below is tbwt_journal.rs2 written out in Go, line for line
// with the script's own strings (TestTbJournalLiteralsAreTheScripts checks every literal against the file in the content checkout),
// and the tests walk it through every state the quest can be in and read each back.

// tbState is the six varps, as the content holds them.
type tbVarps struct {
	main, lubufu, tiadeche, tinsay, tamayu int
	// agility is the doses Tamayu has drunk (bits 3-5 of %tbwt_flags); acceptable, poisoned and kp are bits 6, 7 and 8.
	agility                    int
	acceptable, poisoned, karb bool
}

// journalPieces are the literals tbJournal appends, in the order of the script; the keys are for the test that finds them in the file.
func tbJournal(v tbVarps, jungleAfterSpoken bool) string {
	var b strings.Builder
	add := func(s string) { b.WriteString(s) }
	if v.main < 3 {
		if jungleAfterSpoken {
			add("@dbl@In @dre@Jungle Potion@dbl@, @dre@Trufitus@dbl@ found @dre@Tai Bwo Wannai Village@dbl@ was safe to inhabit. To start this quest I must deliver the news to @dre@Timfraku@dbl@, Chief of the village.||")
			add("@dbl@To complete this quest I need:|")
			add("Level 30 Cooking|Level 15 Agility|Level 5 Fishing|")
			add("High level Agility and Herblore is a distinct advantage.|")
			add("I also need to be able to defeat @dre@level 48 Jogres@dbl@|")
		} else {
			add("@dbl@I can start this quest by speaking to @dre@Timfraku@dbl@, the Chief of @dre@Tai Bwo Wannai Village@dbl@, after completing @dre@Jungle Potion@dbl@.")
		}
		return b.String()
	}
	if v.main == 5 || v.main == 6 {
		add("@red@QUEST COMPLETE!")
		return b.String()
	}
	if v.tamayu > 0 && v.tinsay > 0 && v.tiadeche > 0 {
		add("@str@I have found the three sons of Timfraku:|")
	} else {
		add("@dbl@I need to find the three sons of @dre@Timfraku@dbl@:|")
	}
	for _, n := range []struct {
		name string
		v    int
	}{{"Tamayu", v.tamayu}, {"Tinsay", v.tinsay}, {"Tiadeche", v.tiadeche}} {
		if n.v < 1 {
			add("@dre@" + n.name + "|")
		} else {
			add("@str@" + n.name + "|")
		}
	}
	if v.main == 4 {
		add("@str@I have convinced them to return to the village.||")
		add("@dbl@I must speak with @dre@Timfraku@dbl@.||")
	} else {
		add("@dbl@I need to convince them to return to the village.||")
	}
	// Tiadeche.
	if v.tiadeche > 0 {
		if v.tiadeche == 6 {
			add("@str@Tiadeche:|")
		} else {
			add("@dre@Tiadeche:|")
		}
		if v.tiadeche == 1 {
			add("Nothing of interest.||")
		}
		if v.tiadeche == 3 {
			add("@str@He has successfully caught a Karambwan.||")
		}
		if v.tiadeche == 2 {
			add("@dbl@He will only return to the village once he has caught a|@dre@Karambwan@dbl@.||")
		}
		if v.tiadeche > 4 {
			add("@str@He has successfully caught a Karambwan.|")
			add("@str@I have taken a Karambwan vessel to Tinsay and have|@str@retrieved crafting instructions for Tiadeche.||")
		} else if v.tiadeche == 4 {
			add("@str@He has successfully caught a Karambwan.|")
			add("@dbl@I must take a @dre@Karambwan vessel@dbl@ to @dre@Tinsay@dbl@ and retrieve @dre@crafting instructions@dbl@ for @dre@Tiadeche@dbl@.||")
		}
	}
	// Tinsay.
	if v.tinsay > 0 {
		if v.tinsay == 7 {
			add("@str@Tinsay:|")
		} else {
			add("@dre@Tinsay:|")
		}
		switch v.tinsay {
		case 1:
			add("Nothing of interest.||")
		case 2:
			add("@dbl@He requires @dre@banana in Karamja rum@dbl@ to repair the tribal statue||")
		case 3:
			add("@str@I have given him sliced banana in Karamja rum.||")
		case 4:
			add("@str@I have given him sliced banana in Karamja rum.|")
			add("@dbl@He requires a @dre@seaweed in monkey skin sandwich to repair @dre@the tribal statue with.||")
		case 5:
			add("@str@I have given him sliced banana in Karamja rum.|")
			add("@str@I have given him a seaweed in monkey skin sandwich.||")
		case 6:
			add("@str@I have given him sliced banana in Karamja rum.|")
			add("@str@I have given him a seaweed in monkey skin sandwich.|")
			add("@dbl@He requires @dre@burnt Jogre bones marinated in Karambwanji to ... repair the tribal statue with?||")
		case 7:
			add("@str@I have given him sliced banana in Karamja rum.|")
			add("@str@I have given him a seaweed in monkey skin sandwich.|")
			add("@str@I have given him a burnt Jogre bones marinated in|@str@Karambwanji paste.||")
		}
	}
	// Tamayu.
	if v.tamayu > 0 {
		if v.tamayu == 4 {
			add("@str@Tamayu:|")
		} else {
			add("@dre@Tamayu:|")
		}
		if v.tamayu == 1 {
			add("Nothing of interest.||")
		}
		if v.tamayu == 3 {
			add("@dbl@He will only return to the village once he has slain @dre@Shaikahan@dbl@.|")
			add("@dbl@He appears to be having difficulty in the hunt.||")
			if v.agility >= 4 {
				add("@str@I have increased his agility to match the Shaikahan's.|")
			}
			if v.acceptable && v.karb {
				add("@str@I have give him a stronger and Karambwan poisoned spear.||")
			}
		} else if v.tamayu == 2 {
			add("@dbl@He will only return to the village once he has slain @dre@Shaikahan@dbl@.||")
		}
		if v.tamayu == 4 {
			add("@str@He will only return to the village once he has slain|@str@Shaikahan@dbl@.|")
			add("@str@He appears to be having difficulty in the hunt.||")
			add("@str@I have increased his agility to match the Shaikahan's.|")
			add("@str@I have give him a stronger and Karambwan poisoned spear.||")
			add("@str@Tamayu has slain the Shaikahan!||")
		}
	}
	// Lubufu.
	if v.lubufu > 1 {
		if v.lubufu == 31 {
			add("@str@Lubufu:|")
		} else {
			add("@dre@Lubufu:|")
		}
		add("@str@Lubufu is a crochety old fisherman just south of|@str@Brimhaven.|")
		add("@str@He appears to have a local monopoly over the sale of|@str@Karambwan.||")
		if v.lubufu > 4 {
			add("@str@Lubufu has difficulty collecting bait, due to his age. I have|@str@offered to help collect bait. Lubufu has accepted.|")
			add("@str@Apparently the bait is Karambwanji, which shoal in a Holy|@str@Lake south of Tai Bwo Wannai.||")
		} else if v.lubufu == 4 {
			add("@dbl@Lubufu has difficulty collecting bait, due to his age. I have offered to help collect bait.||")
		}
		if v.lubufu >= 5 && v.lubufu < 25 {
			add("@dbl@I need to give Lubufu 20 Karambwanji.||")
		}
		if v.lubufu >= 25 {
			add("@str@I have given Lubufu 20 Karambwanji.||")
		}
		if v.lubufu > 29 {
			add("@str@Lubufu has offered to train me as his apprentice.|")
			add("@str@I have accepted Lubufu's offer.||")
		} else if v.lubufu == 29 {
			add("@dbl@Lubufu has offered to train me as his apprentice.||")
		}
		if v.lubufu > 29 && v.tiadeche > 2 {
			add("@str@I have betrayed the secret of the Karambwan to Tiadeche!")
		}
	}
	return b.String()
}

// tbLines is the journal as the client holds it: the script's string split at its line breaks.
func tbLines(text string) []string { return strings.Split(text, "|") }

// tbStateWant is what the parser should read from a set of varps: the stage the journal can tell, by the rules of the file's comments.
func tbStateWant(v tbVarps) TbwtState {
	st := TbwtState{Phase: TbPhaseStarted}
	if v.main == 4 {
		st.Phase = TbPhaseBrothers
	}
	switch {
	case v.lubufu <= 1:
		st.Lubufu = TbLubufuNone
	case v.lubufu <= 3:
		st.Lubufu = TbLubufuMet
	case v.lubufu == 4:
		st.Lubufu = TbLubufuOffered
	case v.lubufu < 25:
		st.Lubufu = TbLubufuBait
	case v.lubufu < 29:
		st.Lubufu = TbLubufuGiven
	case v.lubufu == 29:
		st.Lubufu = TbLubufuApprentice
	default:
		st.Lubufu = TbLubufuDone
	}
	switch {
	case v.tiadeche <= 4:
		st.Tiadeche = v.tiadeche
	default:
		st.Tiadeche = TbTiadecheDone
	}
	if st.Tiadeche >= TbTiadecheManual {
		st.Lubufu = TbLubufuDone
	}
	st.Tinsay, st.Tamayu = v.tinsay, v.tamayu
	st.Agility = v.agility >= 4
	st.Spear = v.acceptable && v.karb
	if v.tamayu == 4 {
		st.Agility, st.Spear = true, true
	}
	return st
}

// Every state the quest's brothers can be in reads back as the plan expects, whatever the other three are doing. Tamayu's
// agility and spear flags only exist at his third stage (the page prints them nowhere else).
func TestTbJournalEveryStateReadsBack(t *testing.T) {
	n := 0
	for _, main := range []int{3, 4} {
		for lubufu := 0; lubufu <= 31; lubufu++ {
			for tiadeche := 0; tiadeche <= 6; tiadeche++ {
				for tinsay := 0; tinsay <= 7; tinsay++ {
					for tamayu := 0; tamayu <= 4; tamayu++ {
						for _, fl := range []struct {
							agility          int
							acceptable, karb bool
						}{{0, false, false}, {3, false, false}, {4, false, false}, {4, true, false}, {4, true, true}, {2, true, true}, {0, false, true}} {
							if tamayu != 3 && (fl.agility != 0 || fl.acceptable || fl.karb) {
								continue
							}
							v := tbVarps{main: main, lubufu: lubufu, tiadeche: tiadeche, tinsay: tinsay, tamayu: tamayu, agility: fl.agility,
								acceptable: fl.acceptable, karb: fl.karb}
							got, ok := ParseTaiBwoJournal(tbLines(tbJournal(v, true)))
							if !ok {
								t.Fatalf("%+v: the page was not recognised", v)
							}
							want := tbStateWant(v)
							if TbwtStateOf(got) != want {
								t.Fatalf("%+v: read %v, want %v", v, TbwtStateOf(got), want)
							}
							n++
						}
					}
				}
			}
		}
	}
	t.Logf("%d states read back", n)
}

// The two pages before the job is taken, and the complete page, read as the phases they are; an empty page is unknown.
func TestTbJournalThePhases(t *testing.T) {
	for _, after := range []bool{true, false} {
		got, ok := ParseTaiBwoJournal(tbLines(tbJournal(tbVarps{main: 1}, after)))
		if !ok || TbwtStateOf(got).Phase != TbPhasePre {
			t.Errorf("pre page (Jungle Potion spoken %v): %v %v", after, TbwtStateOf(got), ok)
		}
	}
	got, ok := ParseTaiBwoJournal(tbLines(tbJournal(tbVarps{main: 6}, true)))
	if !ok || TbwtStateOf(got).Phase != TbPhaseComplete {
		t.Errorf("complete page: %v %v", TbwtStateOf(got), ok)
	}
	for _, empty := range [][]string{nil, {""}, {"  ", ""}, {"something else entirely"}} {
		if n, ok := ParseTaiBwoJournal(empty); ok {
			t.Errorf("%q was read as stage %d", empty, n)
		}
	}
}

// The packed stage keeps every field.
func TestTbwtStatePacksAndUnpacks(t *testing.T) {
	for _, st := range []TbwtState{
		{Phase: TbPhaseStarted},
		{Phase: TbPhaseStarted, Lubufu: TbLubufuDone, Tiadeche: TbTiadecheDone, Tinsay: TbTinsayComplete, Tamayu: TbTamayuComplete, Agility: true, Spear: true},
		{Phase: TbPhaseBrothers, Lubufu: TbLubufuBait, Tiadeche: TbTiadecheWaiting, Tinsay: TbTinsayBones, Tamayu: TbTamayuWatched, Spear: true},
		{Phase: TbPhaseComplete, Agility: true},
	} {
		if got := TbwtStateOf(st.Stage()); got != st {
			t.Errorf("%v packed to %d and read back %v", st, st.Stage(), got)
		}
		if st.Stage() == 0 {
			t.Errorf("%v packs to zero, which is what an unread stage is", st)
		}
	}
}

// A page that loses its last lines (the journal holds fifty and the script splits at forty-nine) loses Lubufu's section first, and a
// brother who has asked for a vessel to go to Tinsay can only have had one from Lubufu: he is done whatever the page shows of him.
func TestTbJournalATruncatedLubufuIsDoneOnceTiadecheHasAskedForTheVessel(t *testing.T) {
	v := tbVarps{main: 3, lubufu: 31, tiadeche: 4, tinsay: 4, tamayu: 4, agility: 4, acceptable: true, karb: true}
	full := tbJournal(v, true)
	cut := full[:strings.Index(full, "Lubufu:")]
	got, ok := ParseTaiBwoJournal(tbLines(cut))
	if !ok || TbwtStateOf(got).Lubufu != TbLubufuDone {
		t.Errorf("a page without Lubufu's section at Tiadeche 4: %v %v", TbwtStateOf(got), ok)
	}
	// Before the vessel is asked for, a missing section is a Lubufu not met.
	v = tbVarps{main: 3, lubufu: 0, tiadeche: 2, tinsay: 1, tamayu: 2}
	got, _ = ParseTaiBwoJournal(tbLines(tbJournal(v, true)))
	if TbwtStateOf(got).Lubufu != TbLubufuNone {
		t.Errorf("an unmet Lubufu: %v", TbwtStateOf(got))
	}
}

// wrapLines is how many lines the journal's text takes at a width of about seventy characters (400 pixels of the quest font): each
// line the script breaks is one, and a longer one is as many as its length needs.
func wrapLines(text string) int {
	tag := regexp.MustCompile(`@[a-z0-9]{3}@`)
	n := 0
	for _, l := range strings.Split(text, "|") {
		plain := tag.ReplaceAllString(l, "")
		n += max(1, (len(plain)+69)/70)
	}
	return n
}

// The script splits the journal at forty-nine lines and shows the first page only (quest_journal.rs2: split_init(..., 49, ...),
// split_get(0, n)). The sections before Lubufu's survive every state, and Lubufu's own survives every state in which the module
// still needs it (Tiadeche not past asking for the vessel).
func TestTbJournalLinesFitWhereTheModuleNeedsThem(t *testing.T) {
	worst, worstNeeded := 0, 0
	for lubufu := 0; lubufu <= 31; lubufu++ {
		for tiadeche := 0; tiadeche <= 6; tiadeche++ {
			for tinsay := 0; tinsay <= 7; tinsay++ {
				for tamayu := 0; tamayu <= 4; tamayu++ {
					v := tbVarps{main: 3, lubufu: lubufu, tiadeche: tiadeche, tinsay: tinsay, tamayu: tamayu}
					if tamayu == 3 {
						v.agility, v.acceptable, v.karb = 4, true, true
					}
					text := tbJournal(v, true)
					total := wrapLines(text)
					beforeLubufu := text
					if i := strings.Index(text, "Lubufu:"); i >= 0 {
						beforeLubufu = text[:i]
					}
					before := wrapLines(beforeLubufu)
					worst = max(worst, before)
					if before > 49 {
						t.Fatalf("%+v: %d lines before Lubufu's section", v, before)
					}
					if tiadeche < TbTiadecheManual && lubufu > 1 {
						worstNeeded = max(worstNeeded, total)
						if total > 49 {
							t.Errorf("%+v: %d lines, Lubufu's section is cut while the module still reads it", v, total)
						}
					}
				}
			}
		}
	}
	t.Logf("the longest page before Lubufu's section is %d lines, the longest page the module needs whole is %d (limit 49)", worst, worstNeeded)
}

// contentFile reads a file of the content checkout, CRLF folded to LF; the test skips without RSB0T_CONTENT.
func tbContentFile(t *testing.T, rel ...string) string {
	t.Helper()
	root := os.Getenv("RSB0T_CONTENT")
	if root == "" {
		t.Skip("set RSB0T_CONTENT to a Content checkout")
	}
	raw, err := os.ReadFile(filepath.Join(append([]string{root}, rel...)...))
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(string(raw), "\r\n", "\n")
}

// Every string literal the Go renderer appends is a string literal of tbwt_journal.rs2, so that the renderer cannot drift from the
// script the parser is tested against.
func TestTbJournalLiteralsAreTheScripts(t *testing.T) {
	script := tbContentFile(t, "scripts", "quests", "quest_tbwt", "scripts", "tbwt_journal.rs2")
	src, err := os.ReadFile("taibwo_journal_test.go")
	if err != nil {
		t.Fatal(err)
	}
	renderer := string(src)
	start := strings.Index(renderer, "func tbJournal(")
	end := strings.Index(renderer, "// tbLines is the journal")
	lit := regexp.MustCompile(`add\("((?:[^"\\]|\\.)*)"\)`)
	n := 0
	for _, m := range lit.FindAllStringSubmatch(renderer[start:end], -1) {
		// The names are appended with a prefix in a loop; their literals are checked as the script's own.
		if !strings.Contains(script, m[1]) {
			t.Errorf("the renderer appends %q and tbwt_journal.rs2 has no such string", m[1])
		}
		n++
	}
	for _, name := range []string{"@dre@Tamayu|", "@str@Tamayu|", "@dre@Tinsay|", "@str@Tinsay|", "@dre@Tiadeche|", "@str@Tiadeche|"} {
		if !strings.Contains(script, name) {
			t.Errorf("tbwt_journal.rs2 has no %q", name)
		}
	}
	if n < 40 {
		t.Errorf("only %d literals were found in the renderer: the pattern no longer matches it", n)
	}
	t.Logf("%d literals of the renderer found in the script", n)
	// The stage numbers the renderer branches on are the content's own.
	consts := tbContentFile(t, "scripts", "quests", "quest_tbwt", "configs", "quest_tbwt.constant")
	for _, want := range []string{"^tbwt_tiadeche_return_when_caught_karambwan = 2", "^tbwt_tiadeche_caught_karambwan = 3", "^tbwt_tiadeche_request_manual = 4",
		"^tbwt_tiadeche_complete = 6", "^tbwt_tinsay_complete = 7", "^tbwt_tamayu_complete = 4", "^tbwt_lubufu_fetch_karambwanji = 5",
		"^tbwt_lubufu_given_karambwanji = 25", "^tbwt_lubufu_offered_apprentice = 29", "^tbwt_lubufu_complete = 31", "^tbwt_started = 3",
		"^tbwt_completed_all_brothers = 4"} {
		if !strings.Contains(consts, want) {
			t.Errorf("quest_tbwt.constant has no %q", want)
		}
	}
}

// The needles are written for the normalised text: each is in a literal of the script once colour tags and line breaks are gone.
func TestTbJournalNeedlesAreInTheScript(t *testing.T) {
	script := tbContentFile(t, "scripts", "quests", "quest_tbwt", "scripts", "tbwt_journal.rs2")
	flat := regexp.MustCompile(`@[a-z0-9]{3}@`).ReplaceAllString(script, " ")
	flat = strings.NewReplacer("|", " ", "\\\"", "\"").Replace(flat)
	flat = regexp.MustCompile(`\s+`).ReplaceAllString(strings.ToLower(flat), " ")
	for name, needles := range map[string][]tbwtNeedle{"lubufu": tbwtLubufuNeedles, "tiadeche": tbwtTiadecheNeedles, "tinsay": tbwtTinsayNeedles, "tamayu": tbwtTamayuNeedles} {
		for _, n := range needles {
			if !strings.Contains(flat, n.needle) {
				t.Errorf("%s needle %q is in no line of the script", name, n.needle)
			}
		}
	}
	for _, p := range []string{"i have convinced them to return to the village", "sons of timfraku", "to complete this quest i need", "i can start this quest by speaking to",
		"i have increased his agility", "karambwan poisoned spear", "i have betrayed the secret of the karambwan to tiadeche"} {
		if !strings.Contains(flat, p) {
			t.Errorf("phrase %q is in no line of the script", p)
		}
	}
	// Tamayu's "slain" section opens with the same words in stage 2, 3 and 4 and is told apart by what is printed after them; the
	// needles' order is newest first.
	if got := fmt.Sprint(len(tbwtTamayuNeedles), tbwtTamayuNeedles[0].stage); got != "4 4" {
		t.Errorf("the Tamayu needles start with %s", got)
	}
}
