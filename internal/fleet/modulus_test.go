package fleet

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// FetchProductionModulus takes httpBase as a plain parameter, so its regex/parsing
// logic is testable against a fake HTTP server without touching the real production
// site — the httptest.NewServer pattern internal/controlpanel's own tests already use
// for the same reason (see its helpers_test.go).

func TestFetchProductionModulusExtractsA250DigitRun(t *testing.T) {
	digits := strings.Repeat("7", 260)
	body := "var junk=1;/* client bundle noise */ var n=" + digits + ";moreJunk();"
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/client/client.js" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, body)
	}))
	defer ts.Close()

	mod, err := FetchProductionModulus(ts.URL + "/")
	if err != nil {
		t.Fatalf("FetchProductionModulus: %v", err)
	}
	if mod.String() != digits {
		t.Fatalf("FetchProductionModulus returned %d digits, want the %d-digit run verbatim", len(mod.String()), len(digits))
	}
}

func TestFetchProductionModulusNoMatchIsAnError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "var n=12345;") // far short of the 250-digit heuristic
	}))
	defer ts.Close()

	if _, err := FetchProductionModulus(ts.URL + "/"); err == nil {
		t.Fatal("FetchProductionModulus with no 250+ digit run: want an error, got nil")
	}
}

func TestFetchProductionModulusPropagatesRequestError(t *testing.T) {
	// An httpBase with no listener at all: the initial http.Get must fail and that
	// failure must be wrapped and returned, not silently swallowed.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	badURL := ts.URL + "/"
	ts.Close() // now nothing is listening on this address

	if _, err := FetchProductionModulus(badURL); err == nil {
		t.Fatal("FetchProductionModulus against a closed server: want an error, got nil")
	}
}

func TestFetchProductionModulusPicksTheFirstRunWhenSeveralQualify(t *testing.T) {
	first := strings.Repeat("3", 251)
	second := strings.Repeat("9", 300)
	body := first + ";" + second
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, body)
	}))
	defer ts.Close()

	mod, err := FetchProductionModulus(ts.URL + "/")
	if err != nil {
		t.Fatalf("FetchProductionModulus: %v", err)
	}
	if mod.String() != first {
		t.Fatalf("FetchProductionModulus picked a %d-digit run, want the first match (%d digits)", len(mod.String()), len(first))
	}
}

func TestExtractLoginKeyReadsAStockEngineBundle(t *testing.T) {
	// A stock Engine-TS 289 client.js installs a 512-bit key: a 154-digit modulus and a 77-digit
	// exponent, neither of which the 250+ digit rule alone finds.
	mod, exp := strings.Repeat("9", 154), strings.Repeat("5", 77)
	body := []byte(`this.Eq.Ea($),this.Eq.Ka(BigInt("` + mod + `"),BigInt("` + exp + `")),this.xu.la=0`)
	m, e, err := ExtractLoginKey(body)
	if err != nil {
		t.Fatal(err)
	}
	if m.String() != mod || e.String() != exp {
		t.Fatalf("got %d-digit modulus, exponent %s", len(m.String()), e)
	}
}

func TestExtractLoginKeyKeepsThe250DigitRuleAndReadsItsExponent(t *testing.T) {
	mod := strings.Repeat("8", 309)
	m, e, err := ExtractLoginKey([]byte(`x=BigInt("` + strings.Repeat("1", 120) + `"),BigInt("3");k.Ka(BigInt("` + mod + `"),BigInt("65537"))`))
	if err != nil || m.String() != mod || e.Int64() != 65537 {
		t.Fatalf("got %v %v %v", m, e, err)
	}
	// A bare run with no BigInt pair keeps the old default exponent.
	m, e, err = ExtractLoginKey([]byte("var n=" + mod + ";"))
	if err != nil || m.String() != mod || e.Int64() != 65537 {
		t.Fatalf("bare run: %v %v %v", m, e, err)
	}
}

func TestFetchLoginKeyNamesAMissingKey(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/client/client.js" {
			fmt.Fprint(w, "var n=12345;")
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()
	if _, _, err := FetchLoginKey(ts.URL + "/"); !errors.Is(err, ErrNoLoginKey) {
		t.Fatalf("no key: %v", err)
	}
	// A 404 page (any HTML) is the same answer, not a parse of whatever digits it holds.
	if _, _, err := FetchLoginKey(ts.URL + "/nothing/"); !errors.Is(err, ErrNoLoginKey) {
		t.Fatalf("404: %v", err)
	}
}
