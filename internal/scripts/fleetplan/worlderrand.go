package fleetplan

import (
	"time"
)

// ErrandPurpose names which of the script's mule asks an errand is carrying, so the results can
// be applied by the stage that wanted them after two restarts have wiped its memory.
type ErrandPurpose string

// ErrandPhase is where an errand is.
type ErrandPhase string

// ErrandItem is one line of an errand's kit: one order to the mule (or a few, if Count is above
// PerOrder).
type ErrandItem struct {
	Item  string `json:"item"`
	Count int    `json:"count"`
	// PerOrder bounds one order for this line; 0 means the ask's own default. Food is the line
	// that needs it: a non-stackable order the mule cannot fit in its own pack makes it walk
	// off to bank and cancel the trade (COLLAB, "The mule cannot hand over a non-stackable
	// item"), so the sites already cap food at six an order and the errand keeps their cap.
	PerOrder int `json:"perOrder,omitempty"`
	// Or is the fallback ladder: tried in order, one at a time, only when Item was declined
	// with nothing arriving. A Staff of air falls back to a plain Staff, which still autocasts.
	Or []string `json:"or,omitempty"`
}

// WorldErrand is one errand in flight. Persisted whole.
type WorldErrand struct {
	Purposes  []ErrandPurpose `json:"purposes"`
	Kit       []ErrandItem    `json:"kit"`
	Blocking  bool            `json:"blocking,omitempty"`
	Mule      string          `json:"mule"`
	MuleWorld int             `json:"muleWorld"`
	HomeWorld int             `json:"homeWorld"`
	Phase     ErrandPhase     `json:"phase"`
	// CreatedAt is when the errand was admitted (left the queue): MaxAge counts from here.
	CreatedAt time.Time `json:"createdAt"`
	PhaseAt   time.Time `json:"phaseAt"`
	// QueuedAt is when a queued errand first asked for its slot (QueuedMaxAge counts from here);
	// NextTryAt when it tries again; Tries how many times it was turned away.
	QueuedAt  time.Time `json:"queuedAt,omitempty"`
	NextTryAt time.Time `json:"nextTryAt,omitempty"`
	Tries     int       `json:"tries,omitempty"`
	// ArrivedAt is when the account first stood at the post on this errand (UnsafeLimit).
	ArrivedAt time.Time `json:"arrivedAt,omitempty"`
	// WalkFails counts walks to the post that did not arrive. A stranded account (Karamja,
	// no fare) cannot walk to Catherby at all, and one failed walk used to be the whole cost.
	WalkFails int `json:"walkFails,omitempty"`
	// HopOutRequests/HopHomeRequests count the hops asked for on each leg, so neither leg can
	// be re-requested without bound across restarts; LastHopAt is the latest request.
	HopOutRequests  int       `json:"hopOutRequests,omitempty"`
	HopHomeRequests int       `json:"hopHomeRequests,omitempty"`
	LastHopAt       time.Time `json:"lastHopAt,omitempty"`
	// Asked is true once the ask ran on the mule's world; Got and Declined are what it
	// measured (by pack delta, never by a trade closing).
	Asked    bool              `json:"asked,omitempty"`
	Got      map[string]int    `json:"got,omitempty"`
	Declined map[string]string `json:"declined,omitempty"`
	// Deposited is true once an ErrandDeposit's hand-off ran to its end on the mule's world
	// (whatever it found worth giving), and Gave is what left the pack for the mule.
	Deposited bool           `json:"deposited,omitempty"`
	Gave      map[string]int `json:"gave,omitempty"`
	// Note is the last thing worth saying about this errand, for the log after a restart.
	Note string `json:"note,omitempty"`
}

// HasPurpose reports whether p is one of the errand's purposes.
func (e *WorldErrand) HasPurpose(p ErrandPurpose) bool {
	if e == nil {
		return false
	}
	for _, q := range e.Purposes {
		if q == p {
			return true
		}
	}
	return false
}

// Enter moves the errand to phase at now.
func (e *WorldErrand) Enter(phase ErrandPhase, now time.Time) {
	e.Phase, e.PhaseAt = phase, now
}

// MarketStay is a market hop in flight (markethop.go): the account hopped to the market world to buy
// chaos and goes home when DecideMarketStay says so. Persisted with the world progress.
type MarketStay struct {
	// Want is the chaos the stay is for; BoughtAtStart the account's chaos-bought counter when it
	// began, so Bought = the counter now less this.
	Want          int `json:"want"`
	BoughtAtStart int `json:"boughtAtStart"`
	MarketWorld   int `json:"marketWorld"`
	// StartedAt is when the hop was asked for; Landed that the account has been seen on the market
	// world; HomeRequests how many times the hop home was asked for; LastHopAt the latest request.
	StartedAt    time.Time `json:"startedAt"`
	Landed       bool      `json:"landed,omitempty"`
	HomeRequests int       `json:"homeRequests,omitempty"`
	LastHopAt    time.Time `json:"lastHopAt,omitempty"`
}
