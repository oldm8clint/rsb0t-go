package client

import "testing"

func TestBrowserHeaderStableWithOrigin(t *testing.T) {
	a := BrowserHeader("Acct12", "https://w1.example.invalid/")
	b := BrowserHeader(" acct12 ", "https://w1.example.invalid/")
	if a.Get("User-Agent") != b.Get("User-Agent") || a.Get("Accept-Language") != b.Get("Accept-Language") {
		t.Fatal("headers must not depend on case or surrounding space")
	}
	if got := a.Get("Origin"); got != "https://w1.example.invalid" {
		t.Fatalf("Origin = %q", got)
	}
	agents := map[string]bool{}
	for _, n := range []string{"acct1", "acct2", "acct3", "acct4", "acct5", "acct6", "mule", "main"} {
		agents[BrowserHeader(n, "https://w1.example.invalid/").Get("User-Agent")] = true
	}
	if len(agents) < 3 {
		t.Fatalf("only %d distinct agents over 8 accounts", len(agents))
	}
}

func TestBrowserHeaderNoOriginForBadBase(t *testing.T) {
	if BrowserHeader("a", "").Get("Origin") != "" {
		t.Fatal("no Origin expected without an http base")
	}
}
