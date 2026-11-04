package controlpanel

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func phaseQueryFixture(accounts, samples int) *phaseRecorder {
	now := time.Unix(10000, 0)
	r := newPhaseRecorder(&phaseStub{}, func() time.Time { return now })
	ps := make([]AccountPhase, accounts)
	for i := range ps {
		ps[i] = AccountPhase{Username: fmt.Sprintf("swarm%03d", i), Script: "ScarabSwarm", InGame: true, Running: true}
	}
	for j := 0; j < samples; j++ {
		now = now.Add(time.Second)
		for i := range ps {
			ps[i].Phase = fmt.Sprintf("s3/step%d", j%4)
		}
		r.record(ps, now)
	}
	return r
}

func TestCurrentPhasesMatchesHistoricalQuery(t *testing.T) {
	r := phaseQueryFixture(4, 20)
	r.record([]AccountPhase{{Username: "offline"}, {Username: "event", InGame: true, Running: true, Script: "ScarabSwarm", Event: "Genie"}}, r.now())
	full, current := r.query("1h", "", 30), r.current()
	if full.NowMs != current.NowMs || len(full.Accounts) != len(current.Accounts) {
		t.Fatal("snapshot differs")
	}
	for i, a := range full.Accounts {
		want := currentPhaseJSON{Name: a.Name, Script: a.Script, Phase: a.Phase, Since: a.Since}
		if current.Accounts[i] != want {
			t.Fatalf("current %+v; want %+v", current.Accounts[i], want)
		}
	}
	w := httptest.NewRecorder()
	(&Server{phases: r}).handlePhases(w, httptest.NewRequest(http.MethodGet, "/api/phases?view=current", nil))
	var payload map[string]json.RawMessage
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &payload) != nil {
		t.Fatal(w.Body.String())
	}
	if len(payload) != 2 || payload["now"] == nil || payload["accounts"] == nil {
		t.Fatalf("unexpected fields: %s", w.Body)
	}
	empty := phaseQueryFixture(0, 0).current()
	if empty.Accounts == nil || len(empty.Accounts) != 0 {
		t.Fatal("empty accounts must be an array")
	}
}

func BenchmarkPhaseQueryFleet(b *testing.B) {
	r := phaseQueryFixture(300, 512)
	b.Run("history", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = r.query("1h", "", 30)
		}
	})
	b.Run("current", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = r.current()
		}
	})
}
