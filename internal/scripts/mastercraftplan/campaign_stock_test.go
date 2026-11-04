package mastercraftplan

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/runecraftplan"
)

// A finished campaign's Crafted is its own total, not the next campaign's: the trigger reads CraftedNow, which is 0 for a campaign
// that is not under way (review, 14 Oct 2026: the 100,000 of the last campaign sized the next buy-out at nothing).
func TestCampaignCraftedNowIsZeroUnlessUnderWay(t *testing.T) {
	var c *CampaignLedger
	if c.CraftedNow() != 0 {
		t.Fatal("a nil ledger")
	}
	c = &CampaignLedger{Active: true, Number: 1, Crafted: 31025}
	if c.CraftedNow() != 31025 {
		t.Fatal(c.CraftedNow())
	}
	c.End(runecraftplan.EndTarget, "100,000 crafted", time.Now(), 0)
	if c.Crafted != 31025 || c.CraftedNow() != 0 {
		t.Fatalf("finished: Crafted %d, now %d", c.Crafted, c.CraftedNow())
	}
	// A pause leaves it active: the campaign's own total still counts.
	c = &CampaignLedger{Active: true, Number: 1, Crafted: 12000}
	c.End(runecraftplan.EndTime, "eight hours", time.Now(), 45*time.Minute)
	if c.CraftedNow() != 12000 {
		t.Fatal(c.CraftedNow())
	}
}

// What the stalls had to sell is remembered for a few hours, through a new campaign, and cleared by a buy-out that bought enough.
func TestCampaignNoStockMemory(t *testing.T) {
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	var c CampaignLedger
	if _, held := c.NoStock(now); held {
		t.Fatal("nothing remembered yet")
	}
	c.NoteNoStock(now, 3*time.Hour)
	if until, held := c.NoStock(now.Add(time.Hour)); !held || !until.Equal(now.Add(3*time.Hour)) {
		t.Fatalf("%v %v", until, held)
	}
	if _, held := c.NoStock(now.Add(3*time.Hour + time.Second)); held {
		t.Fatal("it lapses")
	}
	c.NoteNoStock(now, 3*time.Hour)
	c.Begin(now.Add(time.Hour), 40)
	if _, held := c.NoStock(now.Add(time.Hour)); !held || !c.Active || c.Crafted != 0 {
		t.Fatalf("a new campaign starts from zero but the stalls do not restock for it: %+v", c)
	}
	c.NoteStock()
	if _, held := c.NoStock(now.Add(time.Hour)); held || !c.NoStockUntil.IsZero() {
		t.Fatal("cleared")
	}
	var nilLedger *CampaignLedger
	if _, held := nilLedger.NoStock(now); held {
		t.Fatal("nil")
	}
}

// At most one refill without a pause after a gate that ran dry; a round that traded starts the count again; Begin zeroes it.
func TestCampaignDryRefillsAreLimited(t *testing.T) {
	var c CampaignLedger
	if !c.AllowDryRefill(1) || c.DryRefills != 1 {
		t.Fatalf("%+v", c)
	}
	if c.AllowDryRefill(1) || c.DryRefills != 1 {
		t.Fatalf("the second in a row is refused and not counted: %+v", c)
	}
	c.ResetDryRefills()
	if !c.AllowDryRefill(1) {
		t.Fatal("a round that traded allows one again")
	}
	c.Begin(time.Now(), 10)
	if c.DryRefills != 0 {
		t.Fatalf("%+v", c)
	}
}

// The two new fields survive the ledger file, under the strict decode the ledger is read with.
func TestCampaignStockFieldsRoundTrip(t *testing.T) {
	var led Ledger
	until := time.Date(2026, 10, 14, 15, 0, 0, 0, time.UTC)
	led.Rune.Campaign = CampaignLedger{Active: true, Number: 2, NoStockUntil: until, DryRefills: 1}
	raw, err := json.Marshal(led)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"noStockUntil"`) || !strings.Contains(string(raw), `"dryRefills":1`) {
		t.Fatalf("%s", raw)
	}
	var back Ledger
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&back); err != nil {
		t.Fatal(err)
	}
	if c := back.Rune.Campaign; !c.NoStockUntil.Equal(until) || c.DryRefills != 1 || !c.Active {
		t.Fatalf("%+v", c)
	}
	// Absent from an older file: both are zero.
	var old Ledger
	if err := json.Unmarshal([]byte(`{"rune":{"campaign":{"active":true,"number":1}}}`), &old); err != nil || !old.Rune.Campaign.NoStockUntil.IsZero() || old.Rune.Campaign.DryRefills != 0 {
		t.Fatalf("%v %+v", err, old.Rune.Campaign)
	}
}
