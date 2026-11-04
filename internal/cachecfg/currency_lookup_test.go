package cachecfg

import "testing"

func canonicalCoinsConfig(t testing.TB, name string) *Config {
	t.Helper()
	records := make([][]byte, currencyObjID+1)
	for i := range records {
		records[i] = rec(0)
	}
	records[1] = rec(2, "Coins", 11, 0)
	// Preserve the existing unconditional preference for a matching canonical ID,
	// including unusual flags; only the fallback filters flags.
	records[currencyObjID] = rec(2, name, 16, 0)
	oi, od := table(records...)
	ni, nd := table()
	li, ld := table()
	c, err := build(map[string][]byte{"obj.idx": oi, "obj.dat": od, "npc.idx": ni, "npc.dat": nd, "loc.idx": li, "loc.dat": ld})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestCurrencyCanonicalNameAndFallback(t *testing.T) {
	for _, tc := range []struct {
		name string
		want int
	}{
		{"Coins", currencyObjID}, {"cOiNs", currencyObjID},
		{"Other", 1}, {" Coins", 1}, {"Coins ", 1}, {"", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := canonicalCoinsConfig(t, tc.name)
			if got, ok := c.CurrencyID(); !ok || got != tc.want {
				t.Fatalf("CurrencyID() = %d, %v; want %d, true", got, ok, tc.want)
			}
		})
	}
}

var currencyLookupID int
var currencyLookupOK bool

func BenchmarkCanonicalCurrencyLookup(b *testing.B) {
	c := canonicalCoinsConfig(b, "Coins")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		currencyLookupID, currencyLookupOK = c.CurrencyID()
	}
}
