package questrunner

import (
	"strings"
	"sync"
	"time"

	"rsb0t-go/internal/scripts/questplan"
)

// PairLink is a two-account quest's door to its partner (Shield of Arrav, Heroes' Quest). It carries reports only: the partner's side of a trade is
// the partner's own run, and the trade itself goes through the game (questmods/arrav.go), never through this link.
type PairLink interface {
	// State is the pair as this account knows it: Known is false without a configured partner.
	State() questplan.PairState
	// Tell posts this account's report for the partner.
	Tell(t questplan.ArravTell)
	// Heard is the partner's last report and when it was posted.
	Heard() (questplan.ArravTell, time.Time, bool)
}

// PairFresh is how long a partner's report counts as "present now": a waiting account re-posts every few seconds.
const PairFresh = 45 * time.Second

// PairStale is how long an old report is still shown (not as present): a partner that posted an hour ago is a partner that exists.
const PairStale = 6 * time.Hour

// PairBoard is the fleet's shared board of pair reports, by account name (case-insensitive). Every account of one fleet process posts to one
// board, the way the wall bus is shared; it holds the latest report only, so it cannot grow.
type PairBoard struct {
	mu    sync.Mutex
	posts map[string]boardPost
}

type boardPost struct {
	tell questplan.ArravTell
	at   time.Time
}

// NewPairBoard returns an empty board.
func NewPairBoard() *PairBoard { return &PairBoard{posts: map[string]boardPost{}} }

func (b *PairBoard) post(name string, t questplan.ArravTell) {
	b.mu.Lock()
	b.posts[strings.ToLower(strings.TrimSpace(name))] = boardPost{tell: t, at: time.Now()}
	b.mu.Unlock()
}

func (b *PairBoard) read(name string) (questplan.ArravTell, time.Time, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	p, ok := b.posts[strings.ToLower(strings.TrimSpace(name))]
	if !ok || time.Since(p.at) > PairStale {
		return questplan.ArravTell{}, time.Time{}, false
	}
	return p.tell, p.at, true
}

type boardLink struct {
	b           *PairBoard
	me, partner string
	gang        questplan.Gang
	world       func() int
}

// Link is the pair link of account me with its partner on this board. An empty or equal partner is a link that reports no pair.
func (b *PairBoard) Link(me, partner string, world func() int) PairLink {
	g, ok := questplan.PairAssign(me, partner)
	if !ok {
		g = questplan.GangNone
	}
	return &boardLink{b: b, me: me, partner: partner, gang: g, world: world}
}

func (l *boardLink) myWorld() int {
	if l.world == nil {
		return 0
	}
	return l.world()
}

func (l *boardLink) State() questplan.PairState {
	if l.gang == questplan.GangNone {
		return questplan.PairState{}
	}
	st := questplan.PairState{Known: true, Partner: l.partner, Gang: l.gang, World: l.myWorld()}
	if t, at, ok := l.b.read(l.partner); ok {
		st.PartnerWorld = t.World
		st.View = t.View(t.Waiting && time.Since(at) <= PairFresh && (t.World == 0 || st.World == 0 || t.World == st.World))
	}
	return st
}

func (l *boardLink) Tell(t questplan.ArravTell) {
	t.Gang = l.gang
	t.World = l.myWorld()
	l.b.post(l.me, t)
}

func (l *boardLink) Heard() (questplan.ArravTell, time.Time, bool) { return l.b.read(l.partner) }
