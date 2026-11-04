package crowdplay

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/anticheat"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/twitchplays"
)

// countingSession records every packet an action would send, and sends none.
type countingSession struct {
	state  *gamestate.State
	cheats anticheat.Counters
	sent   int
}

func (c *countingSession) Send(build func(w *packet.Packet)) error {
	build(packet.NewWriter(64))
	c.sent++
	return nil
}
func (c *countingSession) State() *gamestate.State     { return c.state }
func (c *countingSession) Cheats() *anticheat.Counters { return &c.cheats }

type fakeGame struct {
	mu       sync.Mutex
	snap     gamestate.RenderSnapshot
	bot      string
	accounts []string
	sess     countingSession
}

func (g *fakeGame) Snapshot(account string) (gamestate.RenderSnapshot, error) { return g.snap, nil }
func (g *fakeGame) Run(account string, fn func(actions.Session) error) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.accounts = append(g.accounts, account)
	if g.sess.state == nil {
		g.sess.state = gamestate.New()
	}
	return fn(&g.sess)
}
func (g *fakeGame) BotRunning(string) string { return g.bot }
func (g *fakeGame) Pack() *pathfind.Pack     { return nil }
func (g *fakeGame) Names() *cachecfg.Config  { return nil }
func (g *fakeGame) sent() int                { g.mu.Lock(); defer g.mu.Unlock(); return g.sess.sent }

func newTestRunner(t *testing.T, g Game, replies *[]string) *Runner {
	t.Helper()
	now := time.Unix(1_700_000_000, 0)
	r, err := NewRunner(Account, g, Options{PerUser: 5 * time.Second, Global: 100 * time.Millisecond, QueueLimit: 3,
		Reply: func(s string) { *replies = append(*replies, s) }, Now: func() time.Time { return now }, Moderators: map[string]bool{"mod": true}})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func msg(id, author, text string, at time.Time) twitchplays.DiscordMessage {
	return twitchplays.DiscordMessage{ID: id, AuthorID: author, AuthorName: author, Content: text, ChannelID: "1", Received: at}
}

func TestOnlyTheCrowdplayAccount(t *testing.T) {
	for _, name := range []string{"mulecon", "Crowdplay", "crowdplay2", "", "fisher1"} {
		if _, err := NewRunner(name, &fakeGame{}, Options{}); err == nil {
			t.Errorf("NewRunner(%q) accepted", name)
		}
	}
	if _, err := NewRunner("crowdplay", &fakeGame{}, Options{}); err != nil {
		t.Errorf("crowdplay refused: %v", err)
	}
}

func TestForbiddenCommandsSendNothingAndAreAnswered(t *testing.T) {
	g := &fakeGame{snap: gamestate.RenderSnapshot{X: 3200, Z: 3200}}
	var replies []string
	r := newTestRunner(t, g, &replies)
	at := time.Unix(1_700_000_000, 0)
	for i, text := range []string{"!bank", "!trade someone", "!drop all", "!give bob coins", "!deposit", "!withdraw coins", "!sell fish", "!giveaway"} {
		r.OnMessage(msg(fmt.Sprint("f", i), fmt.Sprint("viewer", i), text, at.Add(time.Duration(i)*10*time.Second)))
	}
	for i := 0; i < 10; i++ {
		r.Step(context.Background())
	}
	if g.sent() != 0 {
		t.Fatalf("forbidden commands sent %d packets", g.sent())
	}
	if st := r.Stats(); st.Forbidden != 8 || st.Commands != 0 {
		t.Fatalf("stats = %+v, want 8 forbidden, 0 commands", st)
	}
	if len(replies) != 8 || !strings.Contains(replies[0], "not allowed") {
		t.Fatalf("replies = %q", replies)
	}
}

func TestWalkRateLimitDedupeAndStop(t *testing.T) {
	g := &fakeGame{snap: gamestate.RenderSnapshot{X: 3200, Z: 3200}}
	var replies []string
	r := newTestRunner(t, g, &replies)
	at := time.Unix(1_700_000_000, 0)
	r.OnMessage(msg("w1", "alice", "!walk n 3", at))
	r.OnMessage(msg("w1", "alice", "!walk n 3", at)) // the same message again (a reconnect replay)
	r.OnMessage(msg("w2", "alice", "!walk s 3", at.Add(time.Second)))
	r.OnMessage(msg("w3", "bob", "!walk e 11", at.Add(2*time.Second)))
	r.OnMessage(msg("w4", "bob", "hello everyone", at.Add(3*time.Second)))
	r.Step(context.Background())
	if g.sent() != 1 || g.accounts[0] != Account {
		t.Fatalf("walk sent %d packets on %v, want 1 on crowdplay", g.sent(), g.accounts)
	}
	st := r.Stats()
	if st.Duplicate != 1 || st.RateLimited != 1 || st.Invalid != 1 || st.Executed != 1 {
		t.Fatalf("stats = %+v", st)
	}
	// Stop drops everything queued.
	r.OnMessage(msg("w5", "carol", "!walk w 2", at.Add(10*time.Second)))
	r.OnMessage(msg("w6", "dave", "!walk e 2", at.Add(11*time.Second)))
	r.OnMessage(msg("w7", "mod", "!stop", at.Add(11*time.Second+10*time.Millisecond)))
	for i := 0; i < 5; i++ {
		r.Step(context.Background())
	}
	if g.sent() != 1 {
		t.Fatalf("stop did not drop the queue: %d packets", g.sent())
	}
}

func TestBusyWhileABotRuns(t *testing.T) {
	g := &fakeGame{snap: gamestate.RenderSnapshot{X: 3200, Z: 3200}, bot: "ProgressiveFisher"}
	var replies []string
	r := newTestRunner(t, g, &replies)
	r.OnMessage(msg("b1", "alice", "!walk n", time.Unix(1_700_000_000, 0)))
	r.Step(context.Background())
	if g.sent() != 0 || r.Stats().Busy != 1 {
		t.Fatalf("acted while a bot runs: sent %d, stats %+v", g.sent(), r.Stats())
	}
}

func TestInteractRefusesBankTargetsAndNeedsNames(t *testing.T) {
	g := &fakeGame{snap: gamestate.RenderSnapshot{X: 3200, Z: 3200, NPCs: []gamestate.NPC{{Index: 5, Type: 1, X: 3201, Z: 3200}}}}
	var replies []string
	r := newTestRunner(t, g, &replies)
	at := time.Unix(1_700_000_000, 0)
	r.OnMessage(msg("i1", "alice", "!interact banker", at))
	r.OnMessage(msg("i2", "bob", "!interact bank booth", at.Add(time.Second)))
	r.OnMessage(msg("i3", "carol", "!interact goblin", at.Add(2*time.Second)))
	r.OnMessage(msg("i4", "dave", "!fish", at.Add(3*time.Second)))
	for i := 0; i < 4; i++ {
		r.Step(context.Background())
	}
	if g.sent() != 0 {
		t.Fatalf("sent %d packets", g.sent())
	}
	st := r.Stats()
	if st.Invalid != 2 || st.Failed != 2 {
		t.Fatalf("stats = %+v, want the two bank targets invalid and two failed without npc names", st)
	}
}

func TestTokenNeverPrints(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "token.txt")
	if err := os.WriteFile(p, []byte("  abc.def.ghi\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tok, err := ReadTokenFile(p)
	if err != nil || tok.Empty() {
		t.Fatalf("read: %v", err)
	}
	for _, s := range []string{fmt.Sprint(tok), fmt.Sprintf("%v %+v %#v %s", tok, tok, tok, tok)} {
		if strings.Contains(s, "abc") {
			t.Fatalf("token printed: %s", s)
		}
	}
	if tok.authorization() != "Bot abc.def.ghi" {
		t.Fatal("authorization header wrong")
	}
	if err := os.WriteFile(p, []byte("two words"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadTokenFile(p); err == nil || strings.Contains(err.Error(), "two") {
		t.Fatalf("bad token file: %v", err)
	}
	if _, err := ReadTokenFile(filepath.Join(dir, "missing")); err == nil {
		t.Fatal("missing file accepted")
	}
}
