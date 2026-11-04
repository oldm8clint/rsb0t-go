package controlpanel

import (
	"fmt"
	"net/http"
	"reflect"
	"testing"
)

func TestBulkConnectionsSelectsAccountsAndPreservesStartupQueue(t *testing.T) {
	for _, tc := range []struct {
		action string
		want   []string
	}{
		{"login", []string{"offline", "stopped", "error"}},
		{"requeue", []string{"reconnecting", "error"}},
		{"logout", []string{"ingame", "connecting", "reconnecting"}},
	} {
		t.Run(tc.action, func(t *testing.T) {
			src := &staticSource{}
			for _, state := range States {
				src.accounts = append(src.accounts, AccountStatus{Name: string(state), State: state})
			}
			h := newHarness(t, Config{}, testOptions(), connectionControlSource{src})
			var body bulkConnectionBody
			decode(t, h.request(http.MethodPost, "/api/fleet/"+tc.action+"-accounts", nil, nil), &body)
			calls := src.connects
			if tc.action == "logout" {
				calls = src.disconnects
			}
			if !reflect.DeepEqual(calls, tc.want) || !body.OK || body.Changed != len(tc.want) || body.Skipped != len(States)-len(tc.want) || body.SkippedQueued != 1 {
				t.Fatalf("result %+v calls=%v wanted=%v", body, calls, tc.want)
			}
		})
	}
}

type failingConnectionSource struct{ connectionControlSource }

func (s failingConnectionSource) ConnectExisting(name string) error {
	if name == "failed" {
		return fmt.Errorf("fixture refused")
	}
	return s.connectionControlSource.ConnectExisting(name)
}

func TestBulkConnectionsReportsPartialFailuresAndWholeRoster(t *testing.T) {
	src := &staticSource{accounts: []AccountStatus{{Name: "failed", State: StateOffline}}}
	for i := 0; i < 266; i++ {
		src.accounts = append(src.accounts, AccountStatus{Name: fmt.Sprint(i), State: StateOffline})
	}
	h := newHarness(t, Config{}, testOptions(), failingConnectionSource{connectionControlSource{src}})
	var body bulkConnectionBody
	decode(t, h.request(http.MethodPost, "/api/fleet/login-accounts", nil, nil), &body)
	if body.OK || body.Changed != 266 || len(src.connects) != 266 || body.Errors["failed"] != "fixture refused" {
		t.Fatalf("result %+v calls=%d", body, len(src.connects))
	}
}

func TestBulkConnectionsRequiresPostCapabilityAndSingleBulkOwner(t *testing.T) {
	h := newHarness(t, Config{}, testOptions(), connectionControlSource{&staticSource{}})
	var meta metaBody
	decode(t, h.get("/api/meta"), &meta)
	if !meta.BulkConnectionControl {
		t.Fatal("bulk endpoint capability omitted")
	}
	if r := h.get("/api/fleet/login-accounts"); r.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("GET status=%d", r.StatusCode)
	}
	h.srv.bulkConnectionMu.Lock()
	r := h.request(http.MethodPost, "/api/fleet/login-accounts", nil, nil)
	h.srv.bulkConnectionMu.Unlock()
	if r.StatusCode != http.StatusConflict {
		t.Fatalf("overlapping bulk action status=%d", r.StatusCode)
	}
	unsupported := newHarness(t, Config{}, testOptions(), &staticSource{})
	decode(t, unsupported.get("/api/meta"), &meta)
	if meta.BulkConnectionControl {
		t.Fatal("unsupported fleet advertised bulk controls")
	}
	if r := unsupported.request(http.MethodPost, "/api/fleet/login-accounts", nil, nil); r.StatusCode != http.StatusNotImplemented {
		t.Fatalf("unsupported status=%d", r.StatusCode)
	}
}
