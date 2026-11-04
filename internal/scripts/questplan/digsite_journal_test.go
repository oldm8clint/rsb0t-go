package questplan

import (
	"fmt"
	"strings"
	"testing"
)

// digJournalState is what quests/quest_itexam/scripts/itexam_journal.rs2 reads: the progress, the three errands (1 green, 2 purple, 3 orange: the
// script's own numbering) and the pack items the stage 1 and stage 2 paragraphs look at.
type digJournalState struct {
	progress   int
	e1, e2, e3 int
	stamped    bool
	s1, s2, s3 bool // rock_sample1 (green), rock_sample3 (purple, errand 2), rock_sample2 (orange, errand 3) in the pack
}

// digRenderJournal is itexam_journal.rs2 for a started quest, with its `|` line breaks and colour tags, written out branch by branch.
func digRenderJournal(st digJournalState) string {
	x := "@str@I should speak to the Examiner about taking Earth|@str@Science Exams.|"
	p := st.progress
	if p > DigStamping {
		x += "|@str@I should take the letter the Examiner has given me|@str@to the Curator of the Varrock Museum for his approval.|"
		x += "|@str@I need to return the letter of recommendation from the|@str@Curator of Varrock Museum, to the Examiner at the|@str@Exam Centre for inspection.|"
	} else if p == DigStamping {
		if st.stamped {
			x += "|@str@I should take the letter the Examiner has given me|@str@to the Curator of the Varrock Museum for his approval.|"
			x += "|@dbl@I need to return the @dre@letter of recommendation @dbl@from the @dre@Curator of Varrock Museum@dbl@, to the @dre@Examiner @dbl@at the|@dbl@Exam Centre for inspection.|"
		} else {
			x += "|@dbl@I should take the @dre@letter @dbl@the @dre@Examiner @dbl@has given me to the @dre@Curator of the Varrock Museum @dbl@for his approval."
		}
	}
	ready := st.e1 == 2 && st.e2 == 2 && st.e3 == 2
	if p > DigFirstExam {
		x += "|@str@I have passed my first Earth Science exam.|"
	} else if p == DigFirstExam {
		x += "|@dbl@I need to study for my first exam. Perhaps the @dre@students @dbl@on the digsite can help?|"
		switch st.e1 {
		case 2:
			x += "|@str@I need to speak to the student in the green top about the|@str@exams."
			x += "|@str@He gave me an answer to one of the questions on the first|@str@exam."
		case 1:
			x += "|@str@I need to speak to the student in the green top about the|@str@exams."
			if st.s1 {
				x += "|@str@I have agreed to help the student in the green top."
				x += "|@str@He has lost his rock sample and thinks he may have|@str@dropped it around the digsite.|@str@I need to find it and return it to him."
				x += "|@dbl@I should talk to him to see if he can help with any exams."
			} else {
				x += "|@dbl@I have agreed to help the student in the green top.|"
				x += "He has lost his rock sample and thinks he may have dropped it around the digsite.|I need to find it and return it to him."
			}
		default:
			x += "|@dbl@I need to speak to the student in the green top about the exams."
		}
		switch st.e2 {
		case 2:
			x += "|@str@I need to speak to the student in the purple shirt about|@str@the exams."
			x += "|@str@She gave me an answer to one of the questions on the|@str@first exam."
		case 1:
			x += "|@str@I need to speak to the student in the purple shirt about|@str@the exams."
			if st.s2 {
				x += "|@str@I agreed to help the student in the purple shirt."
				x += "|@str@She has lost her rock sample and thinks she may have|@str|dropped it in a bush. I need to find it and return it to her."
				x += "|@dbl@I should talk to her to see if she can help with any exams."
			} else {
				x += "|@dbl@I agreed to help the student in the purple shirt."
				x += "|She has lost her rock sample and thinks she may have dropped it in a bush. I need to find it and return it to her."
			}
		default:
			x += "|@dbl@I need to speak to the student in the purple shirt about the exams."
		}
		switch st.e3 {
		case 2:
			x += "|@str@I need to speak to the student in the orange top about the|@str@exams."
			x += "|@str@He gave me an answer to one of the questions on the first|@str@exam."
		case 1:
			x += "|@str@I need to speak to the student in the orange top about the|@str@exams."
			if st.s3 {
				x += "|@str@I have agreed to help the student in the orange top."
				x += "|@str@He has lost his rock sample and thinks he may have|@str@dropped it in the water.|@str@I need to find it and return it to him."
				x += "|@dbl@I should talk to him to see if he can help with any exams."
			} else {
				x += "|@dbl@I have agreed to help the student in the orange top."
				x += "|He has lost his rock sample and thinks he may have dropped it in the water.|I need to find it and return it to him."
			}
		default:
			x += "|@dbl@I need to speak to the student in the orange top about the exams."
		}
		if ready {
			x += "|@dbl@I should talk to the Examiner to take my first exam. If I have forgotten anything I can always ask the students again."
		}
	}
	if p > DigSecondExam {
		x += "|@str@I have passed my second Earth Science exam.|"
	} else if p == DigSecondExam {
		x += "|@dbl@I need to study for my second exam. Perhaps the three students on the digsite can help me again?|"
		x += digStudentLines(st.e1, st.e2, st.e3, false)
		if ready {
			x += "|@dbl@I should talk to the Examiner to take my second exam. If I have forgotten anything I can always ask the students again."
		}
	}
	if p > DigThirdExam {
		x += "|@str@I have passed my third Earth Science exam.|"
	} else if p == DigThirdExam {
		x += "|@dbl@I need to study for my third exam. Perhaps the three students on the digsite can help me again?|"
		x += digStudentLines(st.e1, st.e2, st.e3, true)
		if ready {
			x += "|@dbl@I should talk to the Examiner to take my third exam. If I have forgotten anything I can always ask the students again."
		}
	}
	if p > DigImpressExpert {
		x += "|@str@I need a find from the site to impress the archeological|@str@expert at the Exam Centre."
		x += "|@str@I need to take the letter to a workman near a winch.|"
	} else if p == DigImpressExpert {
		x += "|@dbl@I need a find from the site to impress the @dre@archeological expert at the Exam Centre."
		x += "|@dbl@I need to take the @dre@letter @dbl@to a workman near a @dre@winch."
	}
	if p > DigMineshaftPermit {
		x += "|@str@I need to investigate the dig shafts."
		x += "|@str@I need to find a way to move the rocks blocking the way|@str@in the shaft. Perhaps someone else in these dig shafts|@str@can help me.|"
	} else if p == DigMineshaftPermit {
		x += "|@dbl@I need to @dre@investigate the dig shafts."
		x += "|@dbl@I need to find a way to move the rocks blocking the way in the shaft. Perhaps someone else in these dig shafts can help me."
	}
	if p > DigPouredCompound {
		x += "|@str@I covered the rocks in the cave with an explosive compound.|"
		x += "|@str@I need to ignite the explosive compound and blow up the|@str@rocks blocking the way.|"
	} else if p == DigPouredCompound {
		x += "|@str@I covered the rocks in the cave with an explosive compound.|"
		x += "|@dbl@I need to ignite the explosive compound and blow up the rocks blocking the way."
	}
	if p > DigRemovedBlockage {
		x += "|@str@I should look for an interesting find in the secret room I|@str@found, and show it to the archeological expert|@str@at the Exam Centre.|"
	} else if p == DigRemovedBlockage {
		x += "|@dbl@I should look for an interesting find in the secret room I found, and show it to @dre@the archeological expert@dbl@ at the Exam Centre."
	}
	if p == DigComplete {
		x += "|@str@The expert was impressed with the Zarosian tablet that I|@str@found, and I also discovered an ancient altar!"
		x += "|@str@I was rewarded for my findings."
		x += "|@str@My work here is done."
		x += "|@dbl@I should also talk to the expert about any other finds."
		x += "|@red@QUEST COMPLETE!"
	}
	return x
}

// digStudentLines are the second and third exams' paragraphs about the three students.
func digStudentLines(e1, e2, e3 int, third bool) string {
	x := ""
	if e1 == 2 {
		x += "|@str@I need to speak to the student in the green top about the|@str@exams."
	} else {
		x += "|@dbl@I need to speak to the student in the green top about the exams."
	}
	switch {
	case e2 == 2:
		x += "|@str@I need to speak to the student in the purple shirt about|@str@the exams."
	case e2 == 1 && third:
		x += "|@str@I need to speak to the student in the purple shirt about|@str@the exams."
		x += "|@dbl@I need to bring her an opal."
	default:
		x += "|@dbl@I need to speak to the student in the purple shirt about the exams."
	}
	if e3 == 2 {
		x += "|@str@I need to speak to the student in the orange top about the|@str@exams."
	} else {
		x += "|@dbl@I need to speak to the student in the orange top about the exams."
	}
	return x
}

// digWrap breaks a journal the way the client's text splitter does for a long line (width 400 in a 12 point font is about 60 characters), the
// colour tag only on the first piece of a line.
func digWrap(page string, width int) []string {
	var lines []string
	for _, seg := range strings.Split(page, "|") {
		for len(seg) > width {
			cut := strings.LastIndex(seg[:width], " ")
			if cut <= 0 {
				break
			}
			lines = append(lines, seg[:cut])
			seg = seg[cut+1:]
		}
		lines = append(lines, seg)
	}
	return lines
}

// digParse parses a rendered page both as the script's own lines and wrapped, and requires the same answer.
func digParse(t *testing.T, what, page string) (stage, flags int) {
	t.Helper()
	raw, ok := ParseDigsiteJournal(strings.Split(page, "|"))
	rawWrapped, okWrapped := ParseDigsiteJournal(digWrap(page, 58))
	if ok != okWrapped || raw != rawWrapped {
		t.Errorf("%s: the script's lines read %d (%v) and the wrapped ones %d (%v)", what, raw, ok, rawWrapped, okWrapped)
	}
	if !ok {
		t.Errorf("%s: not recognised: %q", what, page)
		return 0, 0
	}
	return DigStageOf(raw), DigFlagsOf(raw)
}

// Every stage reads as itself, whatever the errands say, and the flags are the ones the page can tell (see ParseDigsiteJournal).
func TestDigsiteJournalStages(t *testing.T) {
	for stage := DigStamping; stage <= DigComplete; stage++ {
		for _, st := range []digJournalState{{progress: stage}, {progress: stage, stamped: true}, {progress: stage, e1: 2, e2: 2, e3: 2}} {
			got, _ := digParse(t, fmt.Sprintf("stage %d %+v", stage, st), digRenderJournal(st))
			if got != stage {
				t.Errorf("a page of stage %d (%+v) reads as stage %d", stage, st, got)
			}
		}
	}
	// Nothing recognised is unknown, never stage 0.
	for _, lines := range [][]string{nil, {}, {""}, {"Rune Mysteries"}} {
		if raw, ok := ParseDigsiteJournal(lines); ok {
			t.Errorf("%q read as %d", lines, raw)
		}
	}
	if raw, ok := ParseDigsiteJournal(strings.Split("@dbl@I can start this quest by speaking to the @dre@Examiner @dbl@at the @dre@Digsite Exam Centre.|", "|")); !ok || DigStageOf(raw) != DigNotStarted {
		t.Errorf("the not-started page reads %d (%v)", raw, ok)
	}
}

// The first exam's page says which errands are answered; the other stages' pages cannot (the answered and unanswered lines read alike once the
// colour tags are gone) and so carry only the ready line, and the third exam's the opal.
func TestDigsiteJournalFlags(t *testing.T) {
	type c struct {
		st    digJournalState
		flags int
	}
	for _, tc := range []c{
		{digJournalState{progress: DigFirstExam}, 0},
		{digJournalState{progress: DigFirstExam, e1: 1}, 0},
		{digJournalState{progress: DigFirstExam, e1: 1, s1: true, e2: 1, e3: 1}, 0},
		{digJournalState{progress: DigFirstExam, e1: 2}, DigFlagGreen},
		{digJournalState{progress: DigFirstExam, e2: 2}, DigFlagPurple},
		{digJournalState{progress: DigFirstExam, e3: 2}, DigFlagOrange},
		{digJournalState{progress: DigFirstExam, e1: 2, e3: 2}, DigFlagGreen | DigFlagOrange},
		{digJournalState{progress: DigFirstExam, e1: 2, e2: 2}, DigFlagGreen | DigFlagPurple},
		{digJournalState{progress: DigFirstExam, e1: 2, e2: 2, e3: 2}, DigFlagGreen | DigFlagPurple | DigFlagOrange | DigFlagReady},
		{digJournalState{progress: DigFirstExam, e1: 2, e2: 1, s2: true, e3: 2}, DigFlagGreen | DigFlagOrange},
		{digJournalState{progress: DigSecondExam}, 0},
		{digJournalState{progress: DigSecondExam, e1: 2, e2: 2}, 0},
		{digJournalState{progress: DigSecondExam, e1: 2, e2: 2, e3: 2}, DigFlagReady},
		{digJournalState{progress: DigThirdExam, e1: 2, e3: 2}, 0},
		{digJournalState{progress: DigThirdExam, e1: 2, e2: 1, e3: 2}, DigFlagOpal},
		{digJournalState{progress: DigThirdExam, e1: 2, e2: 2, e3: 2}, DigFlagReady},
		{digJournalState{progress: DigImpressExpert, e1: 2, e2: 2, e3: 2}, 0},
		{digJournalState{progress: DigMineshaftPermit}, 0},
		{digJournalState{progress: DigPouredCompound}, 0},
		{digJournalState{progress: DigRemovedBlockage}, 0},
	} {
		_, flags := digParse(t, fmt.Sprintf("%+v", tc.st), digRenderJournal(tc.st))
		if flags != tc.flags {
			t.Errorf("%+v: flags %05b, want %05b", tc.st, flags, tc.flags)
		}
	}
}

// DigRaw packs and unpacks every stage and flag set.
func TestDigRawRoundTrips(t *testing.T) {
	for stage := 0; stage <= DigComplete; stage++ {
		for flags := 0; flags < 1<<5; flags++ {
			raw := DigRaw(stage, flags)
			if DigStageOf(raw) != stage || DigFlagsOf(raw) != flags {
				t.Errorf("DigRaw(%d, %05b) = %d reads back %d, %05b", stage, flags, raw, DigStageOf(raw), DigFlagsOf(raw))
			}
		}
	}
}

// The journal shows 49 lines (split_init(..., 400, 49, q8_full)) and keeps every earlier paragraph on the page, so a long quest could push
// the newest paragraph, where the stage's marker is, off the end. Wrapped at a narrow 58 characters (the real width is nearer 80) every
// stage up to the last that is read off the page still fits; the completed page is not read (the quest tab's colour says complete).
func TestDigsiteJournalPagesFitTheInterface(t *testing.T) {
	for stage := DigStamping; stage <= DigRemovedBlockage; stage++ {
		page := digRenderJournal(digJournalState{progress: stage, e1: 2, e2: 2, e3: 2, stamped: true})
		if n := len(digWrap(page, 58)); n > 49 {
			t.Errorf("the page of stage %d is %d lines, the interface shows 49", stage, n)
		}
	}
}
