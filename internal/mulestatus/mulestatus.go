// Package mulestatus is the MuleReceiver's structured account of itself for the control panel's
// mule dashboard, in two deliberately separate halves:
//
//   - Tracker is LIVE state only: donors that announced a hand-off and are on the way, the trade
//     in progress with its partner and stage, recent demand, and the timing of the last few
//     trade cycles. The mule bot (internal/bot/mule.go) owns one and publishes it through
//     bot.Context.Publish; internal/fleet hands Snapshot() to the control panel.
//   - Trade history is NOT kept here: it is the fleet's persisted trade record,
//     internal/muleledger, which the mule writes through bot.Context.TradeLog. Summarize turns
//     the ledger's entries into the dashboard's rates, success rate, declines by reason, items
//     received and hand-out list.
//
// The one thing the ledger cannot say is how long a trade took (an entry carries only when it
// finished), so the Tracker keeps a small ring of cycle timings — when the mule answered, when
// the items were confirmed, when they were banked — for the throughput tool's measures
// (tools/mule-throughput.py): the median request->accept and accept->banked, and busy %. That
// ring holds no partners or items; it is not a second trade record.
//
// Nothing here parses a log line. Every list is bounded. No I/O and no session: both halves are
// unit-tested with a fake clock.
package mulestatus

import (
	"sort"
	"strings"
	"sync"
	"time"

	"rsb0t-go/internal/muleledger"
	"rsb0t-go/internal/scripts/mulereceiverplan"
)

// Trade stages, in the order a trade passes through them.
const (
	StageReciprocating = "reciprocating" // the mule answered a trade request
	StageOffer         = "offer"         // offer screen open, waiting for the donor's offer to settle
	StageAccepted      = "accepted"      // the mule accepted the offer, waiting for the confirm screen
	StageConfirm       = "confirm"       // confirm screen checked and accepted, waiting for the close
	StageReceipt       = "receipt"       // trade closed, verifying the items actually arrived
)

const (
	maxDonorSeen = 1024
	maxFailures  = 10
	maxHandouts  = 20
	maxItems     = 40  // distinct item names in a summary's received list
	maxCycles    = 256 // cycle timings kept: about three hours at the measured 1.5 trades a minute
	medianOf     = 50  // cycle-time medians use the last this-many completed trades
)

// Incoming is one donor that announced a hand-off over the wall bus and has not traded yet.
type Incoming struct {
	Name    string `json:"name"`
	SinceMs int64  `json:"since"`
}

// Trade is the trade in progress.
type Trade struct {
	Partner      string `json:"partner"`
	Stage        string `json:"stage"`
	SinceMs      int64  `json:"since"`
	StageSinceMs int64  `json:"stageSince"`
}

// ItemCount is one named stack.
type ItemCount struct {
	Name  string `json:"name"`
	Count int64  `json:"count"`
}

// Stock is what the mule's last bank read says it holds, sorted into the groups the dashboard
// shows. Filled by the control panel from the fleet's bank cache (ClassifyStock), not by the bot.
type Stock struct {
	UpdatedMs int64       `json:"updated"`
	Raw       []ItemCount `json:"raw"`
	Cooked    []ItemCount `json:"cooked"`
	Coins     int64       `json:"coins"`
	// Catalog is every hand-out catalogue item (the Scarab kit, the fishing tools) and how many
	// are banked, zero included, so an empty row is visible rather than absent.
	Catalog []ItemCount `json:"catalog"`
	// OtherStacks counts every remaining banked row, for a sense of scale.
	OtherStacks int `json:"otherStacks"`
}

// Failure is one trade that did not complete.
type Failure struct {
	AtMs    int64  `json:"at"`
	Partner string `json:"partner"`
	Reason  string `json:"reason"`
}

// ReasonCount is how many trades failed for one reason.
type ReasonCount struct {
	Reason string `json:"reason"`
	Count  int    `json:"count"`
}

// HandoutRecord is one item the mule gave to a donor.
type HandoutRecord struct {
	AtMs  int64  `json:"at"`
	Item  string `json:"item"`
	Count int64  `json:"count"`
	To    string `json:"to"`
}

// Summary is what Summarize reads out of a mule's ledger entries over a window.
type Summary struct {
	// WindowMs is the span the "Hour" figures cover: one hour, or less just after start.
	WindowMs      int64 `json:"windowMs"`
	CompletedHour int   `json:"completedHour"`
	FailedHour    int   `json:"failedHour"`
	// Completed/Failed count every receive entry the ledger returned.
	Completed int `json:"completed"`
	Failed    int `json:"failed"`
	// SuccessRate is completed / (completed + failed) over the window; -1 when there were none.
	SuccessRate float64 `json:"successRate"`
	// Reasons is every failure reason over the window with its count, most frequent first:
	// the decline rate by reason. A failure the trade code gave no reason for reads "unknown".
	Reasons        []ReasonCount `json:"reasons"`
	Failures       []Failure     `json:"failures"`
	StacksReceived int           `json:"stacksReceived"`
	// Received is every item the window's completed trades brought in, summed by name, largest
	// first (at most maxItems names).
	Received []ItemCount `json:"received"`
	// Partners is how many distinct donors traded over the window.
	Partners int             `json:"partners"`
	Handouts []HandoutRecord `json:"handouts"`
	// HandoutsTotal counts every delivered hand-out the ledger returned, HandoutsFailed the rest.
	HandoutsTotal  int `json:"handoutsTotal"`
	HandoutsFailed int `json:"handoutsFailed"`
}

// Timing is the cycle breakdown from the Tracker's own ring, over the same window as DonorsHour.
type Timing struct {
	// Cycles is how many completed trades the medians are over.
	Cycles int `json:"cycles"`
	// NegotiateMedianMs is request->accept: the donor drawing and settling its offer while the
	// mule only waits. BankMedianMs is accept->banked: the mule banking, unable to trade.
	NegotiateMedianMs int64 `json:"negotiateMedianMs"`
	BankMedianMs      int64 `json:"bankMedianMs"`
	// BusyMs is time inside trade engagements and their banking over the window: the
	// throughput tool's "busy %" numerator.
	BusyMs int64 `json:"busyMs"`
}

// Status is the dashboard's view of one mule. The Tracker fills the live fields; the control
// panel adds History (from the ledger), Stock, TimeSplit and Phase.
type Status struct {
	Account   string     `json:"account"`
	StartedMs int64      `json:"started"`
	Incoming  []Incoming `json:"incoming"`
	Trade     *Trade     `json:"trade,omitempty"`
	// DonorsHour is how many distinct donors announced a hand-off or traded over the last hour
	// (or since start): demand. Compare with the history's completed trades (supply).
	DonorsHour int    `json:"donorsHour"`
	WindowMs   int64  `json:"windowMs"`
	Timing     Timing `json:"timing"`
	// Catalog is the hand-out catalogue's item names, so stock can be sorted without the bot.
	Catalog []string `json:"catalog,omitempty"`

	History   *Summary         `json:"history,omitempty"`
	Stock     *Stock           `json:"stock,omitempty"`
	TimeSplit map[string]int64 `json:"timeSplit,omitempty"`
	Phase     string           `json:"phase,omitempty"`
}

// cycle is one trade engagement's timing: answered, items confirmed (0 for a failure), banked
// (0 until banked), ended.
type cycle struct {
	start, accept, banked, end int64
}

// Tracker is one mule's live state. Safe for concurrent use: the bot writes from its own
// goroutine while the fleet reads Snapshot from the control panel's. Every method is a no-op on
// a nil Tracker, so a caller never needs a nil check to report.
type Tracker struct {
	mu      sync.Mutex
	now     func() time.Time
	account string
	started int64

	incoming  map[string]Incoming // keyed by normalised name
	trade     *Trade
	donorSeen map[string]int64
	catalog   []string

	cycles  []cycle // oldest first, at most maxCycles
	banking int     // index in cycles of a completed trade whose items are being banked, or -1
}

// NewTracker starts a tracker for account. now may be nil (time.Now).
func NewTracker(account string, now func() time.Time) *Tracker {
	if now == nil {
		now = time.Now
	}
	return &Tracker{now: now, account: account, started: now().UnixMilli(), incoming: map[string]Incoming{}, donorSeen: map[string]int64{}, banking: -1}
}

// key normalises a player name the way usernames compare: case, surrounding space and
// space-versus-underscore ignored.
func key(name string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(name)), "_", " ")
}

// SetCatalog records the hand-out catalogue's item names.
func (t *Tracker) SetCatalog(names []string) {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.catalog = append([]string(nil), names...)
	t.mu.Unlock()
}

// SetIncoming replaces the announced-donor list with names -> when each announced.
func (t *Tracker) SetIncoming(announced map[string]time.Time) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	next := make(map[string]Incoming, len(announced))
	for name, when := range announced {
		k := key(name)
		if prev, ok := t.incoming[k]; ok && prev.SinceMs == when.UnixMilli() {
			next[k] = prev
		} else {
			next[k] = Incoming{Name: name, SinceMs: when.UnixMilli()}
		}
		t.seeDonor(k, when.UnixMilli())
	}
	t.incoming = next
}

func (t *Tracker) seeDonor(k string, atMs int64) {
	if k == "" {
		return
	}
	if _, ok := t.donorSeen[k]; !ok && len(t.donorSeen) >= maxDonorSeen {
		cutoff := t.now().Add(-time.Hour).UnixMilli()
		for name, when := range t.donorSeen {
			if when < cutoff {
				delete(t.donorSeen, name)
			}
		}
		if len(t.donorSeen) >= maxDonorSeen {
			return
		}
	}
	if atMs > t.donorSeen[k] {
		t.donorSeen[k] = atMs
	}
}

// Stage records that the trade with partner reached stage. The first stage of a new partner
// starts an engagement.
func (t *Tracker) Stage(partner, stage string) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now().UnixMilli()
	if t.trade == nil || (partner != "" && t.trade.Partner != "" && key(partner) != key(t.trade.Partner)) {
		t.trade = &Trade{Partner: partner, Stage: stage, SinceMs: now, StageSinceMs: now}
		return
	}
	if t.trade.Partner == "" {
		t.trade.Partner = partner
	}
	if t.trade.Stage != stage {
		t.trade.Stage = stage
		t.trade.StageSinceMs = now
	}
}

func (t *Tracker) pushCycle(c cycle) {
	if len(t.cycles) >= maxCycles {
		n := copy(t.cycles, t.cycles[1:])
		t.cycles = t.cycles[:n]
		if t.banking >= 0 {
			t.banking--
		}
	}
	t.cycles = append(t.cycles, c)
}

// Completed records that the trade with partner closed with every promised stack received; its
// cycle stays open until Banked.
func (t *Tracker) Completed(partner string) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now().UnixMilli()
	start := now
	if t.trade != nil {
		start = t.trade.SinceMs
	}
	t.pushCycle(cycle{start: start, accept: now, end: now})
	t.banking = len(t.cycles) - 1
	t.seeDonor(key(partner), now)
	delete(t.incoming, key(partner))
	t.trade = nil
}

// Banked records that the last completed trade's items are deposited.
func (t *Tracker) Banked() {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.banking < 0 {
		return
	}
	now := t.now().UnixMilli()
	t.cycles[t.banking].banked, t.cycles[t.banking].end = now, now
	t.banking = -1
}

// Failed records that the trade with partner did not complete; why is the ledger's business.
func (t *Tracker) Failed(partner string) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now().UnixMilli()
	start := now
	if t.trade != nil {
		start = t.trade.SinceMs
		if partner == "" {
			partner = t.trade.Partner
		}
	}
	t.seeDonor(key(partner), now)
	t.pushCycle(cycle{start: start, end: now})
	t.trade = nil
}

// Idle clears any trade in progress without recording it (a stop mid-trade, or a cycle that
// never reached a partner).
func (t *Tracker) Idle() {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.trade = nil
	t.mu.Unlock()
}

// Snapshot is the tracker's live Status.
func (t *Tracker) Snapshot() Status {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now().UnixMilli()
	from := now - time.Hour.Milliseconds()
	if from < t.started {
		from = t.started
	}
	s := Status{Account: t.account, StartedMs: t.started, WindowMs: now - from, Catalog: append([]string(nil), t.catalog...)}
	for _, in := range t.incoming {
		s.Incoming = append(s.Incoming, in)
	}
	sort.Slice(s.Incoming, func(i, j int) bool { return s.Incoming[i].SinceMs < s.Incoming[j].SinceMs })
	if t.trade != nil {
		tr := *t.trade
		s.Trade = &tr
	}
	for _, when := range t.donorSeen {
		if when >= from {
			s.DonorsHour++
		}
	}
	var negotiate, banking []int64
	for i := len(t.cycles) - 1; i >= 0; i-- {
		c := t.cycles[i]
		if c.accept > 0 && c.banked > 0 && len(negotiate) < medianOf {
			negotiate = append(negotiate, c.accept-c.start)
			banking = append(banking, c.banked-c.accept)
		}
		a := c.start
		if a < from {
			a = from
		}
		if c.end > a {
			s.Timing.BusyMs += c.end - a
		}
	}
	s.Timing.Cycles = len(negotiate)
	s.Timing.NegotiateMedianMs, s.Timing.BankMedianMs = median(negotiate), median(banking)
	return s
}

// sumItems adds items into into by name, keeping at most maxItems names (the rest are dropped),
// largest first.
func sumItems(into []ItemCount, items []muleledger.Item) []ItemCount {
	for _, it := range items {
		found := false
		for i := range into {
			if into[i].Name == it.Name {
				into[i].Count += it.Count
				found = true
				break
			}
		}
		if !found && len(into) < maxItems {
			into = append(into, ItemCount{Name: it.Name, Count: it.Count})
		}
	}
	sort.SliceStable(into, func(i, j int) bool { return into[i].Count > into[j].Count })
	return into
}

func median(v []int64) int64 {
	if len(v) == 0 {
		return 0
	}
	sort.Slice(v, func(i, j int) bool { return v[i] < v[j] })
	if len(v)%2 == 1 {
		return v[len(v)/2]
	}
	return (v[len(v)/2-1] + v[len(v)/2]) / 2
}

// Summarize reads a mule's ledger entries (any order) into the dashboard's figures over the last
// hour before now, or since startedMs when that is later.
func Summarize(entries []muleledger.Entry, now time.Time, startedMs int64) Summary {
	nowMs := now.UnixMilli()
	from := nowMs - time.Hour.Milliseconds()
	if startedMs > from {
		from = startedMs
	}
	// Newest first; entries at the same millisecond keep their arrival order reversed, so the
	// last one recorded still reads as the newest.
	recs := make([]muleledger.Entry, len(entries))
	for i, r := range entries {
		recs[len(entries)-1-i] = r
	}
	sort.SliceStable(recs, func(i, j int) bool { return recs[i].AtMs > recs[j].AtMs })
	s := Summary{WindowMs: nowMs - from, SuccessRate: -1}
	partners := map[string]bool{}
	reasons := map[string]int{}
	for _, r := range recs {
		inWindow := r.AtMs >= from
		ok := r.Outcome == muleledger.OutcomeAccepted
		if r.Kind == muleledger.KindHandout {
			if !ok {
				s.HandoutsFailed++
				continue
			}
			s.HandoutsTotal++
			if len(s.Handouts) < maxHandouts {
				h := HandoutRecord{AtMs: r.AtMs, To: r.Partner}
				if len(r.Out) > 0 {
					h.Item, h.Count = r.Out[0].Name, r.Out[0].Count
				}
				s.Handouts = append(s.Handouts, h)
			}
			continue
		}
		reason := r.Reason
		if reason == "" {
			reason = "unknown"
		}
		if ok {
			s.Completed++
			s.StacksReceived += len(r.In)
		} else {
			s.Failed++
			if len(s.Failures) < maxFailures {
				s.Failures = append(s.Failures, Failure{AtMs: r.AtMs, Partner: r.Partner, Reason: reason})
			}
		}
		if !inWindow {
			continue
		}
		if ok {
			s.CompletedHour++
			s.Received = sumItems(s.Received, r.In)
		} else {
			s.FailedHour++
			reasons[reason]++
		}
		if r.Partner != "" {
			partners[key(r.Partner)] = true
		}
	}
	if n := s.CompletedHour + s.FailedHour; n > 0 {
		s.SuccessRate = float64(s.CompletedHour) / float64(n)
	}
	s.Partners = len(partners)
	for reason, n := range reasons {
		s.Reasons = append(s.Reasons, ReasonCount{Reason: reason, Count: n})
	}
	sort.Slice(s.Reasons, func(i, j int) bool {
		return s.Reasons[i].Count > s.Reasons[j].Count || (s.Reasons[i].Count == s.Reasons[j].Count && s.Reasons[i].Reason < s.Reasons[j].Reason)
	})
	return s
}

// ClassifyStock sorts a bank read into raw fish, cooked fish, coins and the hand-out catalogue.
// names/counts are parallel: each banked row's display name and count. Fish are recognised by
// mulereceiverplan.FishTiers (the nine fish the fleet hands over) plus any other "Raw " name.
func ClassifyStock(names []string, counts []int64, catalog []string, updated time.Time) Stock {
	cookedNames := map[string]bool{}
	for _, tier := range mulereceiverplan.FishTiers {
		cookedNames[key(tier.Cooked)] = true
	}
	inCatalog := map[string]int{}
	st := Stock{UpdatedMs: updated.UnixMilli()}
	for i, name := range catalog {
		inCatalog[key(name)] = i
		st.Catalog = append(st.Catalog, ItemCount{Name: name})
	}
	raw, cooked := map[string]int64{}, map[string]int64{}
	for i, name := range names {
		if i >= len(counts) {
			break
		}
		n, k := counts[i], key(name)
		if j, ok := inCatalog[k]; ok {
			st.Catalog[j].Count += n
			if k == "coins" {
				st.Coins += n
			}
			continue
		}
		switch {
		case k == "coins":
			st.Coins += n
		case strings.HasPrefix(k, "raw "):
			raw[name] += n
		case cookedNames[k]:
			cooked[name] += n
		default:
			st.OtherStacks++
		}
	}
	for name, n := range raw {
		st.Raw = append(st.Raw, ItemCount{Name: name, Count: n})
	}
	for name, n := range cooked {
		st.Cooked = append(st.Cooked, ItemCount{Name: name, Count: n})
	}
	byCount := func(v []ItemCount) {
		sort.Slice(v, func(i, j int) bool {
			return v[i].Count > v[j].Count || (v[i].Count == v[j].Count && v[i].Name < v[j].Name)
		})
	}
	byCount(st.Raw)
	byCount(st.Cooked)
	return st
}
