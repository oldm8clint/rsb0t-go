package controlpanel

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/fleet"
)

// With no proxy pool the status body is exactly what it was: no "routes" key at all. With one,
// /api/status carries each route's counters under logins.routes — id, kind and host label,
// never a credential.
func TestStatusReportsRoutesOnlyWithAPool(t *testing.T) {
	plain := fleet.New(fleet.Config{WSURL: "ws://localhost/"})
	defer plain.CloseAll()
	body := string(encodeStatus(nil, NewFleetSource(plain, []string{"a"}, time.Second).Logins()))
	if strings.Contains(body, "routes") || strings.Contains(body, `"route"`) {
		t.Fatalf("no pool, yet the status mentions routes: %s", body)
	}

	const secret = "panel-must-never-show-this"
	pool, err := fleet.ParseRoutePool([]byte(`{"routes": [
		{"id": "warp", "type": "socks5", "addr": "127.0.0.1:40000"},
		{"id": "paid", "type": "socks5", "addr": "proxy.example.net:1080", "username": "svc-user", "password": "`+secret+`"}]}`),
		func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	m := fleet.New(fleet.Config{WSURL: "ws://localhost/", Routes: pool})
	defer m.CloseAll()
	body = string(encodeStatus(nil, NewFleetSource(m, []string{"a"}, time.Second).Logins()))
	for _, want := range []string{`"routes":[`, `"id":"direct"`, `"id":"warp","kind":"socks5","host":"127.0.0.1","max":89`, `"host":"proxy.example.net"`, `"health":"healthy"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("status lacks %s: %s", want, body)
		}
	}
	if strings.Contains(body, secret) || strings.Contains(body, "svc-user") || strings.Contains(body, ":1080") {
		t.Fatalf("status leaks a credential or full address: %s", body)
	}
}
