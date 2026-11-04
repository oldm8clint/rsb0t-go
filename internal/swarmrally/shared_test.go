package swarmrally

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestSharedReturnsTheSameRally(t *testing.T) {
	defer Forget("kq-test")
	a, created, err := Shared("kq-test", DefaultPolicy())
	if err != nil || !created {
		t.Fatalf("first Shared: created=%v err=%v", created, err)
	}
	b, created2, err := Shared("kq-test", Policy{Quorum: 3, MinEngage: 1, MaxWait: time.Second, Stale: time.Second})
	if err != nil || created2 {
		t.Fatalf("second Shared: created=%v err=%v", created2, err)
	}
	if a != b {
		t.Fatal("two calls with the same name returned different rallies, which would split the cohort")
	}
	// The second caller's policy is ignored on purpose: the first account through the door sets
	// the rule. Prove it, because a silently-taken second policy is exactly how a cohort ends up
	// with two different quorums.
	if got := b.Snapshot().Policy.Quorum; got != DefaultPolicy().Quorum {
		t.Fatalf("quorum = %d, want the FIRST caller's %d", got, DefaultPolicy().Quorum)
	}
}

func TestSharedRefusesAnEmptyName(t *testing.T) {
	if _, _, err := Shared("", DefaultPolicy()); err == nil {
		t.Fatal("an empty rally name was accepted; it would pool every script in the process")
	}
}

func TestSharedRefusesAnUnterminatingPolicy(t *testing.T) {
	defer Forget("bad")
	if _, _, err := Shared("bad", Policy{Quorum: 5, MinEngage: 1, MaxWait: 0, Stale: time.Second}); err == nil {
		t.Fatal("a rally with no deadline was created")
	}
	if Lookup("bad") != nil {
		t.Fatal("the refused rally was registered anyway")
	}
}

func TestLookupNeverCreates(t *testing.T) {
	if Lookup("never-opened") != nil {
		t.Fatal("Lookup created a rally")
	}
}

func TestSharedIsSafeUnderRacingCreators(t *testing.T) {
	defer Forget("race")
	var wg sync.WaitGroup
	got := make([]*Rally, 50)
	creates := make([]bool, 50)
	for i := 0; i < 50; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, c, err := Shared("race", DefaultPolicy())
			if err != nil {
				t.Error(err)
				return
			}
			got[i], creates[i] = r, c
		}()
	}
	wg.Wait()
	n := 0
	for _, c := range creates {
		if c {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("%d of 50 racing callers believed they created the rally, want exactly 1", n)
	}
	for i, r := range got {
		if r != got[0] {
			t.Fatalf("caller %d got a different rally", i)
		}
	}
}

func TestNamesListsOpenRallies(t *testing.T) {
	defer Forget("named-a")
	defer Forget("named-b")
	if _, _, err := Shared("named-a", DefaultPolicy()); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Shared("named-b", DefaultPolicy()); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, n := range Names() {
		seen[n] = true
	}
	for _, want := range []string{"named-a", "named-b"} {
		if !seen[want] {
			t.Errorf("Names() is missing %q (got %v)", want, Names())
		}
	}
}

// Two cohorts in one process must not pool into one quorum.
func TestTwoNamedRalliesAreIndependent(t *testing.T) {
	defer Forget("cohort-1")
	defer Forget("cohort-2")
	p := Policy{Quorum: 3, MinEngage: 1, MaxWait: time.Hour, Stale: time.Hour}
	a, _, _ := Shared("cohort-1", p)
	b, _, _ := Shared("cohort-2", p)
	for i := 0; i < 3; i++ {
		a.Report(fmt.Sprintf("a%d", i), true)
	}
	if !a.Engaged() {
		t.Fatal("cohort-1 did not engage at its quorum")
	}
	if b.Engaged() {
		t.Fatal("cohort-2 engaged on cohort-1's members")
	}
}
