package randomevents

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/pathfind"
)

// kickUnreachableOldMan runs n guardian polls against an Old Man one tile east whose latest
// overhead line is speech, on an empty pack so every approach answers "no approach path" at
// once (the live the mule failure, without waiting on a real walk). The retry window each failed
// approach sets is cleared between polls, as the next poll 600ms on would find it expired.
func kickUnreachableOldMan(t *testing.T, speech string, n int, age time.Duration) (lines []string, cooledDown bool) {
	t.Helper()
	withResolver(t, fakeResolver{npcNames: map[int]string{500: "Mysterious Old Man"}, npcOps: map[int][5]string{500: {"Talk-to"}}})
	s := &overlapSession{state: overlapState()}
	setSelfName(t, s.state, "android")
	addGreetingNPC(s.state, 500, 32768+5, speech)
	g := NewGuardian(s, &pathfind.Pack{})
	defer g.Stop()
	g.Log = func(l string) { lines = append(lines, l) }
	ev := g.nextEvent()
	if ev == nil || ev.Name != "Mysterious Old Man" {
		t.Fatalf("setup: the Old Man was not detected: %+v", ev)
	}
	sig := eventSignature(ev)
	for i := 0; i < n; i++ {
		g.mu.Lock()
		delete(g.retryUntil, sig)
		if age > 0 && g.attempts[sig] > 0 {
			g.firstAttempt[sig] = time.Now().Add(-age)
		}
		g.mu.Unlock()
		g.kick()
	}
	return lines, g.cooledDown(sig)
}

func gaveUp(lines []string) bool {
	for _, l := range lines {
		if strings.Contains(l, "gave up on Mysterious Old Man") {
			return true
		}
	}
	return false
}

// Live on build e35420b6 (2026-09-24 18:16:09-21): four "no approach path" answers in four
// seconds on the mule's OWN Mysterious Old Man, a give-up, and 38 seconds later the Old Man
// fail-teleported the fleet's only mule for 3h18m. Its content timer runs 48 seconds, so an
// Old Man that greets this account must keep being tried for as long as it can still be
// answered.
func TestOwnOldManIsNotGivenUpAfterFourQuickFailures(t *testing.T) {
	lines, cooled := kickUnreachableOldMan(t, "Greetings android!", 8, 0)
	if gaveUp(lines) || cooled {
		t.Fatalf("gave up on this account's own Old Man after quick failures inside its timer:\n%s", strings.Join(lines, "\n"))
	}
	if n := strings.Count(strings.Join(lines, "\n"), "random event: Mysterious Old Man — handling"); n != 8 {
		t.Fatalf("want 8 attempts, got %d:\n%s", n, strings.Join(lines, "\n"))
	}
}

// Live 6 Oct 2026 (acct21, Wizards' Tower west room): the same unreachable Old Man answered "no approach path" every
// four seconds for 38 seconds. The attempts go on (the Old Man's own timer is still running), but the error line stops
// after noApproachPathErrors and is replaced by one plain statement that the event is being left to its timer.
func TestUnreachableOldManStopsReportingErrorsAfterAFewTries(t *testing.T) {
	lines, _ := kickUnreachableOldMan(t, "Greetings android!", 8, 0)
	text := strings.Join(lines, "\n")
	if n := strings.Count(text, "handler error: no approach path"); n != noApproachPathErrors {
		t.Fatalf("want %d error lines, got %d:\n%s", noApproachPathErrors, n, text)
	}
	if n := strings.Count(text, "leaving it to its own timer"); n != 1 {
		t.Fatalf("want one leaving-it line, got %d:\n%s", n, text)
	}
}

// The cap: once the run has outlived the Old Man's own timer it has either been answered or
// already teleported the account, and a truly unreachable one still ends.
func TestOwnOldManIsGivenUpOnceItsTimerHasRunOut(t *testing.T) {
	lines, cooled := kickUnreachableOldMan(t, "Greetings android!", maxAttempts+1, ownedFailTeleportWindow+time.Second)
	if !gaveUp(lines) || !cooled {
		t.Fatalf("an own Old Man past its timer must still be given up:\n%s", strings.Join(lines, "\n"))
	}
}

// Nothing changes for an Old Man that names nobody: the ordinary four-attempt give-up stands,
// which is what keeps an account from chasing somebody else's NPC.
func TestUnnamedOldManKeepsTheFourAttemptGiveUp(t *testing.T) {
	lines, cooled := kickUnreachableOldMan(t, "", maxAttempts+1, 0)
	if !gaveUp(lines) || !cooled {
		t.Fatalf("an Old Man naming nobody must still be given up after %d attempts:\n%s", maxAttempts, strings.Join(lines, "\n"))
	}
}

func TestOwnOldManAttemptCountIsCapped(t *testing.T) {
	lines, cooled := kickUnreachableOldMan(t, "Greetings android!", ownedFailTeleportMaxAttempts+1, 0)
	if !gaveUp(lines) || !cooled {
		t.Fatalf("an own Old Man must still be given up after %d attempts:\n%s", ownedFailTeleportMaxAttempts, strings.Join(lines[len(lines)-3:], "\n"))
	}
}
