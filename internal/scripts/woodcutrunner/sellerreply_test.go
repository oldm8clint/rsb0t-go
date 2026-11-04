package woodcutrunner

import (
	"testing"
	"time"

	"rsb0t-go/internal/chatlog"
)

// TestAwaitSellerReply pins how a market stall's public answer is read.
//
// Every line below is VERBATIM from the live Seers stalls on 2026-09-21, captured once the fleet
// could hear public chat. They are the reason this matching is loose rather than exact: the text
// comes back sentence-cased with a trailing space, the stalls phrase themselves inconsistently,
// and the invitation embeds the buyer's own account name ("Trade me, acct223.").
func TestAwaitSellerReply(t *testing.T) {
	for _, c := range []struct {
		name  string
		lines []string
		want  sellerVerdict
	}{
		{
			name:  "the invitation, which is what unblocks the trade",
			lines: []string{"Seers Market: Got your rune axe. Trade me, acct223."},
			want:  sellerReady,
		},
		{
			name: "the quote alone is not an invitation — it is still fetching",
			lines: []string{
				"Seers Market: 1 X rune axe = 150,000gp (150,000ea). Give me a moment.",
			},
			want: sellerSilent,
		},
		{
			name:  "out of stock, so asking twice more is wasted",
			lines: []string{"Tradevault: I have no rune axe right now."},
			want:  sellerRefused,
		},
		{
			name:  "does not stock it at all",
			lines: []string{"Sil Stuff: I don't sell 'rune axe'. "},
			want:  sellerRefused,
		},
		{
			name: "another stall's invitation must not be read as ours",
			lines: []string{
				"Rune Axe: 1 X rune axe = 200,000gp (200,000ea). Trade me.",
			},
			want: sellerSilent,
		},
		{
			name:  "unrelated chatter is ignored",
			lines: []string{"Door Closer: The door demanded pink skirts; 1 of 1 obeyed."},
			want:  sellerSilent,
		},
		{
			// Caught live on 2026-09-21: Acct194 logged "says it has one" quoting
			// "Trade me, acct219." and then requested a trade the stall was not offering it,
			// which is why the seller "never offered" anything. Twelve accounts shouting at one
			// stall read each other's invitations.
			name:  "an invitation addressed to another buyer is not ours",
			lines: []string{"Seers Market: Got your rune axe. Trade me, acct219."},
			want:  sellerSilent,
		},
		{
			name:  "an invitation addressed to us is ours",
			lines: []string{"Seers Market: Got your rune axe. Trade me, acct223."},
			want:  sellerReady,
		},
	} {
		seller := "Seers Market"
		if c.name == "out of stock, so asking twice more is wasted" {
			seller = "Tradevault"
		}
		if c.name == "does not stock it at all" {
			seller = "Sil Stuff"
		}
		// The reply must arrive AFTER the ask, which is the whole point of the cursor: the
		// stall answers something we just said, so lines already in the log are not answers.
		// Delivering them from the Delay callback reproduces that ordering exactly.
		log := chatlog.New()
		delivered := false
		d := &Deps{Chat: log, Delay: func(time.Duration) bool {
			if !delivered {
				for _, l := range c.lines {
					log.Record(l)
				}
				delivered = true
			}
			return true
		}}
		got, _ := awaitSellerReply(d, seller, "Rune axe", "acct223", 50*time.Millisecond)
		if got != c.want {
			t.Errorf("%s: awaitSellerReply = %v, want %v", c.name, got, c.want)
		}
	}
}
