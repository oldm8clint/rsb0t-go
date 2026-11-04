package mulebus

// The Fishing Trawler's presence (FishingTrawler crew and MasterCrafter's trawler round, 1 Nov 2026). It rides the existing
// KindHere message -- "any account reporting its own tile" -- with the trawler role in Text, the same way the Nature gate's
// carriers ride KindRuneCarrier with theirs: a shape and a reader, no policy. Whether the anchor starts a round on what it
// hears is mastercraftplan's business, whether a crew member crosses the gangplank trawlerplan's.
//
//	crew   -> everyone: TrawlerLobby (waiting in the Port Khazard lobby for the anchor's call), TrawlerAboard (aboard, the
//	          anchor not seen), TrawlerAboardAnchor (aboard and the anchor seen aboard), every TrawlerEvery
//	anchor -> everyone: TrawlerBoard (crossing the gangplank now: board) every TrawlerEvery while it waits to be taken aboard;
//	          TrawlerGather (its two-hour trawler block is on: every account of the fleet comes to the lobby on its world)
//	          every TrawlerEvery while the block runs and it is not boarding or aboard
//
// Every message is stamped with the sender's world (OnWorld): one boat per world, so a crew member on another world is no crew.
//
// Watches (3 Nov 2026): the crew is split in two watches that sail alternate games, so one watch walks home from Port Sarim
// while the other plays. The anchor's TrawlerBoard and TrawlerGather carry the watch it calls next, and a crew member's
// TrawlerLobby its own (WithWatch, in Message.Qty); 0 is "any watch".

import (
	"strings"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// The trawler roles, carried in a KindHere message's Text.
const (
	TrawlerLobby        = "trawler-lobby"
	TrawlerAboard       = "trawler-aboard"
	TrawlerAboardAnchor = "trawler-aboard-anchor"
	TrawlerBoard        = "trawler-board"
	TrawlerGather       = "trawler-gather"
)

// TrawlerEvery is how often a crew member or the anchor publishes its role; TrawlerTTL how long one counts after its last
// message (three missed). A minute since 2 Nov 2026 (it was 15 s): the whole fleet of about 350 keeps about a thousand of the
// bus's 3,000 entries inside the window, where every 15 s would have pushed the mule's traffic out.
const (
	TrawlerEvery = time.Minute
	TrawlerTTL   = 3 * TrawlerEvery
)

// IsTrawlerRole reports whether role is one of the five.
func IsTrawlerRole(role string) bool {
	switch role {
	case TrawlerLobby, TrawlerAboard, TrawlerAboardAnchor, TrawlerBoard, TrawlerGather:
		return true
	}
	return false
}

// TrawlerMessage builds the KindHere message of an account at the trawler: from, the tile it stands on, its role and its world.
func TrawlerMessage(from string, at tile.Tile, role string, world int) Message {
	m := HereMessage(from, at)
	m.Text = role
	return OnWorld(m, world)
}

// WithWatch stamps a trawler message with a watch (1 or 2; 0 any), carried in Qty.
func WithWatch(m Message, watch int) Message {
	m.Qty = watch
	return m
}

// WithBucket stamps a crew member's trawler message with whether it holds a bailing bucket, carried in Slots (1: it does).
func WithBucket(m Message, bucket bool) Message {
	m.Slots = 0
	if bucket {
		m.Slots = 1
	}
	return m
}

// TrawlerView is one account's newest trawler message.
type TrawlerView struct {
	Name  string
	Role  string
	Tile  tile.Tile
	World int
	Watch int
	// Bucket is a crew member saying it holds a bailing bucket (WithBucket).
	Bucket bool
	Age    time.Duration
}

// ReadTrawler lists every account's newest trawler message within ttl of now (zero: TrawlerTTL), in the order the accounts
// first appear. A KindHere message without a trawler role (a donor telling a mule where it stands) is not one.
func ReadTrawler(b *Bus, now time.Time, ttl time.Duration) []TrawlerView {
	if b == nil {
		return nil
	}
	if ttl <= 0 {
		ttl = TrawlerTTL
	}
	index := map[string]int{}
	var out []TrawlerView
	for _, e := range b.stampedSince(now.Add(-ttl)) {
		m := e.msg
		if m.Kind != KindHere || !IsTrawlerRole(m.Text) || strings.TrimSpace(m.From) == "" {
			continue
		}
		v := TrawlerView{Name: m.From, Role: m.Text, Tile: m.Tile, World: m.World, Watch: m.Qty, Bucket: m.Slots == 1, Age: now.Sub(e.at)}
		key := trimLower(m.From)
		if i, ok := index[key]; ok {
			out[i] = v
			continue
		}
		index[key] = len(out)
		out = append(out, v)
	}
	return out
}

// TrawlerLobbyCount is how many accounts other than anchor say they wait in the lobby on world (0: any world) on watch (0: any
// watch; a crew member that named no watch counts for both).
func TrawlerLobbyCount(views []TrawlerView, anchor string, world, watch int) int {
	n := 0
	for _, v := range views {
		if v.Role != TrawlerLobby || NamesMatch(v.Name, clampName(anchor)) {
			continue
		}
		if !SameWatch(v.Watch, watch) {
			continue
		}
		if world > 0 && v.World > 0 && v.World != world {
			continue
		}
		n++
	}
	return n
}

// TrawlerAnchorCalled reports whether anchor's newest message is its board call on world (0: any world) for watch (0: any).
func TrawlerAnchorCalled(views []TrawlerView, anchor string, world, watch int) bool {
	for _, v := range views {
		if !NamesMatch(v.Name, clampName(anchor)) {
			continue
		}
		return v.Role == TrawlerBoard && (world <= 0 || v.World <= 0 || v.World == world) && SameWatch(v.Watch, watch)
	}
	return false
}

// SameWatch reports whether two watches match, 0 matching either.
func SameWatch(a, b int) bool {
	return a <= 0 || b <= 0 || a == b
}

// TrawlerBlockTTL is how long the anchor's last message keeps its trawler block on for the crowd: the claim, the bank and the
// store between games can keep it quiet for minutes.
const TrawlerBlockTTL = 10 * time.Minute

// TrawlerBlock reports whether anchor's trawler block is on and on which world: its newest trawler message, read with
// TrawlerBlockTTL (ReadTrawler(b, now, TrawlerBlockTTL)), is any trawler role. False once it has been quiet that long.
func TrawlerBlock(views []TrawlerView, anchor string) (world int, on bool) {
	for _, v := range views {
		if NamesMatch(v.Name, clampName(anchor)) {
			return v.World, true
		}
	}
	return 0, false
}

// TrawlerAnchorAboard reports whether anchor's newest trawler message says it is aboard (its round publishes TrawlerAboard
// every few seconds while it plays).
func TrawlerAnchorAboard(views []TrawlerView, anchor string) bool {
	for _, v := range views {
		if NamesMatch(v.Name, clampName(anchor)) {
			return v.Role == TrawlerAboard || v.Role == TrawlerAboardAnchor
		}
	}
	return false
}
