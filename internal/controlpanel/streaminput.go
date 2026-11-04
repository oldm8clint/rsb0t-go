package controlpanel

import (
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/anticheat"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/client"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/pathfind"
)

// Opt-in stream client INPUT: a viewer holding the stream view lease may take control of that
// one account and click to walk and interact, sent through the account's EXISTING session.
//
// OFF by default. Nothing here is reachable unless the operator starts the panel with
// -stream-input (Config.StreamInput); without it /api/meta.streamClient.sessionInput stays
// false and /api/stream/input answers 501, exactly as before.
//
// The rules, in order:
//   - Take control (POST /api/stream/input {session, enabled:true[, force]}) needs the view
//     lease and a current session epoch. It is refused while a main or side interface is open
//     (a bank, trade, shop) or when the account runs MuleReceiver, unless force is sent.
//   - If the account's bot is running it is STOPPED first, and the take is only acknowledged
//     once the bot reports it stopped; the script and settings are remembered.
//   - Every action (POST /api/stream/action) must come from that lease, on the current epoch,
//     decided from a recent observation (its seq), at most one per streamActionInterval, and
//     is checked against the current observation and the cache config before anything is sent.
//   - Release — explicit, the view closing, the lease expiring, the session changing — restarts
//     the remembered bot. A panel restart needs nothing remembered: every account's configured
//     script is auto-started at launch (cmd/controlpanel), and a manual-control stop is never
//     persisted, so no account comes back stranded.
//   - Every request, accepted or refused, is audit-logged (GET /api/stream/audit, and the log).

// StreamSessionRunner is the Source capability input needs: run a function on the account's
// existing session (internal/fleet.Manager.WithSession for the real fleet).
type StreamSessionRunner interface {
	WithStreamSession(account string, fn func(actions.Session) error) error
}

// StreamPathSource is an optional Source capability: the collision pack, so a walk click is
// routed around walls (as the game client routes one) instead of sent as a straight line.
type StreamPathSource interface {
	StreamCollisionPack() *pathfind.Pack
}

// StreamCollisionPack implements StreamPathSource from the fleet's configured pack.
func (f *FleetSource) StreamCollisionPack() *pathfind.Pack {
	if f.manager == nil {
		return nil
	}
	return f.manager.CollisionPack()
}

// streamWalkRoute is the corner list for a walk click: a route through the collision pack
// when this build has one (the game client computes its own route the same way before it
// sends MOVE_GAMECLICK), else the straight line the engine then follows as far as it can.
func streamWalkRoute(pack *pathfind.Pack, from, to pathfind.NavPoint) ([]pathfind.NavPoint, string) {
	if pack != nil {
		if r := pack.FindPath(from, to); r.OK && len(r.Waypoints) > 1 {
			return r.Waypoints, "routed"
		}
	}
	return []pathfind.NavPoint{from, to}, "straight"
}

// WithStreamSession implements StreamSessionRunner on the account's existing session.
func (f *FleetSource) WithStreamSession(account string, fn func(actions.Session) error) error {
	if f.manager == nil {
		return errNoManager
	}
	return f.manager.WithSession(account, func(s *client.Session) error { return fn(s) })
}

// WithStreamSession implements StreamSessionRunner for the simulated fleet: there is no game,
// so an action is validated and its packet encoded into a throwaway buffer, then discarded.
func (f *FakeSource) WithStreamSession(account string, fn func(actions.Session) error) error {
	return fn(&discardSession{state: gamestate.New()})
}

type discardSession struct {
	state  *gamestate.State
	cheats anticheat.Counters
	sent   int
}

func (d *discardSession) Send(build func(w *packet.Packet)) error {
	build(packet.NewWriter(64))
	d.sent++
	return nil
}
func (d *discardSession) State() *gamestate.State     { return d.state }
func (d *discardSession) Cheats() *anticheat.Counters { return &d.cheats }

// streamActionInterval rate-limits input: the game itself only moves once per 600 ms tick.
const streamActionInterval = 300 * time.Millisecond

// streamActionMaxAge is how many observations old the one an action was decided from may be
// (about five seconds at the default rate): an action is never decided on a stale view.
const streamActionMaxAge = 25

// streamActionRange bounds walks and loc interactions, in tiles (Chebyshev).
const streamActionRange = 30

const streamAuditSize = 256

type streamControl struct {
	viewID, account string
	epoch           int64
	resumeScript    string
	resumeSettings  map[string]interface{}
	since           time.Time
	lastAction      time.Time
}

type streamAuditEntry struct {
	At      int64  `json:"at"`
	Account string `json:"account"`
	Request string `json:"request"`
	Detail  string `json:"detail,omitempty"`
	Result  string `json:"result"`
}

type streamInputState struct {
	mu      sync.Mutex
	control *streamControl
	audit   []streamAuditEntry
}

// streamInputAvailable: input exists only with the flag AND a Source that can observe the
// session, run an action on it and stop/start its bot.
func (s *Server) streamInputAvailable() bool {
	if !s.cfg.StreamInput {
		return false
	}
	_, runner := s.src.(StreamSessionRunner)
	_, bots := s.src.(BotControl)
	_, render := s.src.(RenderSource)
	return runner && bots && render
}

func (s *Server) auditStream(account, request, detail, result string) {
	e := streamAuditEntry{At: s.opts.now().UnixMilli(), Account: account, Request: request, Detail: detail, Result: result}
	s.input.mu.Lock()
	s.input.audit = append(s.input.audit, e)
	if len(s.input.audit) > streamAuditSize {
		s.input.audit = s.input.audit[len(s.input.audit)-streamAuditSize:]
	}
	s.input.mu.Unlock()
	log.Printf("controlpanel: stream input [%s] %s %s -> %s", account, request, detail, result)
}

// resumeAfterLostRace restarts the bot this request paused when another viewer took control
// between the pause and this request's claim, and audits the outcome. A failed restart used to
// be dropped, leaving the account's bot stopped with nothing in the audit log to say why; the
// release path (which handles the same call) already reported it. Returns the refusal message.
func (s *Server) resumeAfterLostRace(bc BotControl, account string, control *streamControl) string {
	msg := "another viewer took control first"
	if control.resumeScript == "" {
		s.auditStream(account, "take control", "", "refused: "+msg)
		return msg
	}
	result := "refused: " + msg + "; resumed " + control.resumeScript
	if err := bc.StartBot(account, control.resumeScript, control.resumeSettings); err != nil {
		result = "refused: " + msg + "; resuming " + control.resumeScript + " failed: " + err.Error()
		msg += "; resuming the paused bot " + control.resumeScript + " failed: " + err.Error()
	}
	s.auditStream(account, "take control", "", result)
	return msg
}

func (s *Server) handleStreamAudit(w http.ResponseWriter, r *http.Request) {
	if !getOnly(w, r) {
		return
	}
	s.input.mu.Lock()
	out := append([]streamAuditEntry(nil), s.input.audit...)
	s.input.mu.Unlock()
	writeJSON(w, http.StatusOK, struct {
		Enabled bool               `json:"enabled"`
		Entries []streamAuditEntry `json:"entries"`
	}{s.streamInputAvailable(), out})
}

// heldView returns the live view for a lease id, or nil when it is not held or has expired.
func (s *Server) heldView(id string) *streamView {
	now := s.opts.now()
	s.stream.mu.Lock()
	defer s.stream.mu.Unlock()
	v := s.stream.view
	if v == nil || v.id != id || now.Sub(v.lastSeen) >= s.streamLeaseTTL() {
		return nil
	}
	v.lastSeen = now
	cp := *v
	return &cp
}

func (s *Server) accountStatus(name string) *AccountStatus {
	for _, a := range s.src.Accounts(nil) {
		if a.Name == name {
			a := a
			return &a
		}
	}
	return nil
}

// handleStreamControl is /api/stream/input when input is available on this build.
func (s *Server) handleStreamControl(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Session string `json:"session"`
		Enabled bool   `json:"enabled"`
		Force   bool   `json:"force"`
	}
	if !readJSONBody(w, r, 1024, &req) {
		return
	}
	view := s.heldView(req.Session)
	if view == nil {
		s.auditStream("", "control", "", "refused: no view lease")
		writeError(w, http.StatusGone, "this stream client lease is no longer held; open a new one")
		return
	}
	if !req.Enabled {
		released := s.releaseStreamControl(req.Session, "released by the viewer")
		writeJSON(w, http.StatusOK, struct {
			OK           bool `json:"ok"`
			Acknowledged bool `json:"acknowledged"`
			Released     bool `json:"released"`
		}{true, false, released})
		return
	}
	if current, err := s.streamEpochFor(view.account); err != nil || current != view.epoch {
		s.auditStream(view.account, "take control", "", "refused: stale session epoch")
		writeError(w, http.StatusConflict, "this account's session changed; reopen the view before taking control")
		return
	}
	s.input.mu.Lock()
	if c := s.input.control; c != nil {
		s.input.mu.Unlock()
		if c.viewID == req.Session {
			writeJSON(w, http.StatusOK, struct {
				OK           bool `json:"ok"`
				Acknowledged bool `json:"acknowledged"`
			}{true, true})
			return
		}
		s.auditStream(view.account, "take control", "", "refused: another viewer has control")
		writeError(w, http.StatusConflict, "another viewer already has input control")
		return
	}
	s.input.mu.Unlock()

	rs := s.src.(RenderSource)
	snap, err := rs.RenderSnapshot(view.account)
	if err != nil {
		s.auditStream(view.account, "take control", "", "refused: "+err.Error())
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	status := s.accountStatus(view.account)
	if status == nil {
		writeError(w, http.StatusConflict, "unknown account")
		return
	}
	force := ""
	if req.Force {
		force = " (force)"
	}
	if (snap.UI.Main != -1 || snap.UI.Side != -1) && !req.Force {
		s.auditStream(view.account, "take control", fmt.Sprintf("main %d side %d", snap.UI.Main, snap.UI.Side), "refused: an interface (bank, trade, shop) is open")
		writeError(w, http.StatusConflict, fmt.Sprintf("an interface is open on this account (main %d, side %d) — a bank or trade must not be interrupted; send force to override", snap.UI.Main, snap.UI.Side))
		return
	}
	if status.Bot == "MuleReceiver" && !req.Force {
		s.auditStream(view.account, "take control", "", "refused: MuleReceiver")
		writeError(w, http.StatusConflict, "this account runs MuleReceiver; mules are never taken over without force")
		return
	}
	bc := s.src.(BotControl)
	control := &streamControl{viewID: req.Session, account: view.account, epoch: view.epoch, since: s.opts.now()}
	if status.BotState == "running" {
		if err := bc.StopBot(view.account); err != nil {
			s.auditStream(view.account, "take control", "stop "+status.Bot, "refused: "+err.Error())
			writeError(w, http.StatusConflict, "could not stop the account's bot: "+err.Error())
			return
		}
		if !s.waitBotStopped(view.account) {
			// Still running: leave it running rather than end up with two drivers.
			s.auditStream(view.account, "take control", "stop "+status.Bot, "refused: the bot did not stop")
			writeError(w, http.StatusConflict, "the account's bot did not stop; input control refused")
			return
		}
		control.resumeScript, control.resumeSettings = status.Bot, status.Settings
	}
	s.input.mu.Lock()
	if s.input.control != nil {
		s.input.mu.Unlock()
		writeError(w, http.StatusConflict, s.resumeAfterLostRace(bc, view.account, control))
		return
	}
	s.input.control = control
	s.input.mu.Unlock()
	s.auditStream(view.account, "take control"+force, "paused bot "+quoteOrNone(control.resumeScript), "accepted")
	go s.watchStreamControl(control)
	writeJSON(w, http.StatusOK, struct {
		OK           bool   `json:"ok"`
		Acknowledged bool   `json:"acknowledged"`
		PausedBot    string `json:"pausedBot,omitempty"`
	}{true, true, control.resumeScript})
}

func quoteOrNone(s string) string {
	if s == "" {
		return "(none running)"
	}
	return fmt.Sprintf("%q", s)
}

// waitBotStopped polls the account list until its bot is no longer running (3s at most).
func (s *Server) waitBotStopped(account string) bool {
	for i := 0; i < 30; i++ {
		if st := s.accountStatus(account); st == nil || (st.BotState != "running" && st.BotState != "stopping") {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

// releaseStreamControl ends input control (for viewID, or any when empty) and restarts the
// bot it paused. Returns whether control was held.
func (s *Server) releaseStreamControl(viewID, reason string) bool {
	s.input.mu.Lock()
	c := s.input.control
	if c == nil || (viewID != "" && c.viewID != viewID) {
		s.input.mu.Unlock()
		return false
	}
	s.input.control = nil
	s.input.mu.Unlock()
	result := "released"
	if c.resumeScript != "" {
		if bc, ok := s.src.(BotControl); ok {
			if err := bc.StartBot(c.account, c.resumeScript, c.resumeSettings); err != nil {
				result = "released; resuming " + c.resumeScript + " failed: " + err.Error()
			} else {
				result = "released; resumed " + c.resumeScript
			}
		}
	}
	s.auditStream(c.account, "release", reason, result)
	return true
}

// checkStreamControl releases control whose view lease is gone or expired, or whose session
// changed. The watchdog calls it; so do the view handlers.
func (s *Server) checkStreamControl() {
	s.input.mu.Lock()
	c := s.input.control
	s.input.mu.Unlock()
	if c == nil {
		return
	}
	now := s.opts.now()
	s.stream.mu.Lock()
	v := s.stream.view
	gone := v == nil || v.id != c.viewID || now.Sub(v.lastSeen) >= s.streamLeaseTTL()
	s.stream.mu.Unlock()
	if gone {
		s.releaseStreamControl(c.viewID, "view lease ended")
		return
	}
	if current, err := s.streamEpochFor(c.account); err != nil || current != c.epoch {
		s.releaseStreamControl(c.viewID, "session changed")
	}
}

// watchStreamControl makes sure a closed or crashed tab can never leave an account's bot
// stopped: it re-checks the lease every second until control is released.
func (s *Server) watchStreamControl(c *streamControl) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for range t.C {
		s.input.mu.Lock()
		still := s.input.control == c
		s.input.mu.Unlock()
		if !still {
			return
		}
		s.checkStreamControl()
	}
}

type streamAction struct {
	Kind  string `json:"kind"` // walk | npc | player | loc | obj | npcu | playeru | locu | obju | if | count | chat
	X     int    `json:"x"`
	Z     int    `json:"z"`
	Index int    `json:"index"`
	Type  int    `json:"type"`
	ID    int    `json:"id"`
	Op    int    `json:"op"`
	Run   bool   `json:"run"`
	// Interface clicks (streaminterface.go): the game's MiniMenuAction code and the slot and
	// component it names (the object is ID); Use is the item a "Use" was started on, for
	// npcu/playeru/locu/obju and use-item-on-item.
	Menu  int        `json:"menu,omitempty"`
	Slot  int        `json:"slot,omitempty"`
	Com   int        `json:"com,omitempty"`
	Use   *streamUse `json:"use,omitempty"`
	Text  string     `json:"text,omitempty"`
	Value int64      `json:"value,omitempty"`
}

func (s *Server) streamNamesConfig() *cachecfg.Config {
	if n, ok := s.src.(StreamNameSource); ok {
		return n.StreamNames()
	}
	return nil
}

func opLabel(ops [5]string, op int) string {
	if op < 1 || op > 5 {
		return ""
	}
	return ops[op-1]
}

// handleStreamAction validates one click against the current observation and sends it.
func (s *Server) handleStreamAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, "POST")
		return
	}
	if !s.streamInputAvailable() {
		writeError(w, http.StatusNotImplemented, "session input is not enabled on this build (-stream-input)")
		return
	}
	var req struct {
		Session string       `json:"session"`
		Epoch   int64        `json:"epoch"`
		Seq     int64        `json:"seq"`
		Action  streamAction `json:"action"`
	}
	if !readJSONBody(w, r, 2048, &req) {
		return
	}
	a := req.Action
	detail := fmt.Sprintf("%s x=%d z=%d index=%d type=%d id=%d op=%d", a.Kind, a.X, a.Z, a.Index, a.Type, a.ID, a.Op)
	switch a.Kind {
	case "if":
		detail = fmt.Sprintf("if menu=%d obj=%d slot=%d com=%d", a.Menu, a.ID, a.Slot, a.Com)
	case "chat":
		detail = fmt.Sprintf("chat %d characters", len(a.Text))
	case "count":
		detail = fmt.Sprintf("count %d", a.Value)
	}
	if a.Use != nil {
		detail += fmt.Sprintf(" use obj=%d slot=%d com=%d", a.Use.Obj, a.Use.Slot, a.Use.Com)
	}
	refuse := func(account string, status int, msg string) {
		s.auditStream(account, "action", detail, "refused: "+msg)
		writeError(w, status, msg)
	}
	s.checkStreamControl()
	view := s.heldView(req.Session)
	if view == nil {
		refuse("", http.StatusGone, "this stream client lease is no longer held")
		return
	}
	s.input.mu.Lock()
	c := s.input.control
	ok := c != nil && c.viewID == req.Session
	rateOK := ok && s.opts.now().Sub(c.lastAction) >= streamActionInterval
	if rateOK {
		c.lastAction = s.opts.now()
	}
	s.input.mu.Unlock()
	if !ok {
		refuse(view.account, http.StatusConflict, "take input control first")
		return
	}
	if req.Epoch != view.epoch || req.Epoch != c.epoch {
		refuse(view.account, http.StatusConflict, "stale session epoch")
		return
	}
	if req.Seq > view.seq || req.Seq < view.seq-streamActionMaxAge {
		refuse(view.account, http.StatusConflict, fmt.Sprintf("decided from observation %d, current is %d: too old", req.Seq, view.seq))
		return
	}
	if !rateOK {
		refuse(view.account, http.StatusTooManyRequests, "one action per 300 ms")
		return
	}
	snap, err := s.src.(RenderSource).RenderSnapshot(view.account)
	if err != nil {
		refuse(view.account, http.StatusConflict, err.Error())
		return
	}
	near := func(x, z int) bool {
		dx, dz := x-snap.X, z-snap.Z
		return max(dx, -dx) <= streamActionRange && max(dz, -dz) <= streamActionRange
	}
	names := s.streamNamesConfig()
	var send func(actions.Session) error
	label := ""
	switch a.Kind {
	case "walk":
		if !near(a.X, a.Z) {
			refuse(view.account, http.StatusBadRequest, "walk target out of range")
			return
		}
		lvl := snap.Level
		var pack *pathfind.Pack
		if ps, ok := s.src.(StreamPathSource); ok {
			pack = ps.StreamCollisionPack()
		}
		route, how := streamWalkRoute(pack, pathfind.NavPoint{X: snap.X, Z: snap.Z, Level: lvl}, pathfind.NavPoint{X: a.X, Z: a.Z, Level: lvl})
		send = func(ss actions.Session) error { return actions.Walk(ss, route, a.Run) }
		label = "Walk here"
		detail += fmt.Sprintf(" (%s, %d corners)", how, len(route))
	case "npcu", "locu", "obju", "playeru":
		// "Use <item> with <target>": the used item is checked like any interface click.
		coms := s.streamComponents()
		if coms == nil {
			refuse(view.account, http.StatusBadRequest, "this build has no interface archive, so Use cannot be checked")
			return
		}
		if err := validUse(coms, snap.UI, a.Use); err != nil {
			refuse(view.account, http.StatusConflict, err.Error())
			return
		}
		u := *a.Use
		label = "Use " + objName(names, u.Obj) + " with "
		switch a.Kind {
		case "playeru":
			var found *gamestate.RenderPlayer
			for i := range snap.Players {
				if snap.Players[i].Index == a.Index && a.Index != snap.SelfSlot {
					found = &snap.Players[i]
					break
				}
			}
			if found == nil || !near(found.X, found.Z) {
				refuse(view.account, http.StatusConflict, "that player is not in view")
				return
			}
			label += found.Name
			send = s.streamPlayerInteraction(&snap, found, func(ss actions.Session) error { return actions.OpPlayerU(ss, a.Index, u.Obj, u.Slot, u.Com) })
		case "npcu":
			var found *gamestate.NPC
			for i := range snap.NPCs {
				if snap.NPCs[i].Index == a.Index {
					found = &snap.NPCs[i]
				}
			}
			if found == nil {
				refuse(view.account, http.StatusConflict, "that npc is not in view")
				return
			}
			name := fmt.Sprintf("npc %d", found.Type)
			if names != nil {
				if t, ok := names.Npc(found.Type); ok && t.Name != "" {
					name = t.Name
				}
			}
			label += name
			send = func(ss actions.Session) error { return actions.OpNpcU(ss, a.Index, int32(u.Obj), u.Slot, u.Com) }
		case "locu":
			if !near(a.X, a.Z) {
				refuse(view.account, http.StatusBadRequest, "loc out of range")
				return
			}
			name := fmt.Sprintf("loc %d", a.Type)
			if names != nil {
				if t, ok := names.Loc(a.Type); ok && t.Name != "" {
					name = t.Name
				}
			}
			label += name
			send = func(ss actions.Session) error {
				return actions.OpLocU(ss, a.X, a.Z, a.Type, int32(u.Obj), u.Slot, u.Com)
			}
		case "obju":
			present := false
			for _, o := range snap.Objs {
				if o.X == a.X && o.Z == a.Z && o.ID == a.ID && o.Level == snap.Level {
					present = true
				}
			}
			if !present {
				refuse(view.account, http.StatusConflict, "that ground item is not there")
				return
			}
			label += objName(names, a.ID)
			send = func(ss actions.Session) error { return actions.OpObjU(ss, a.X, a.Z, a.ID, int32(u.Obj), u.Slot, u.Com) }
		}
	case "if", "count", "chat":
		fn, l, err := s.streamInterfaceAction(&snap, a)
		if err != nil {
			refuse(view.account, http.StatusConflict, err.Error())
			return
		}
		send, label = fn, l
	case "player":
		var found *gamestate.RenderPlayer
		for i := range snap.Players {
			if snap.Players[i].Index == a.Index && a.Index != snap.SelfSlot {
				found = &snap.Players[i]
				break
			}
		}
		if found == nil || !near(found.X, found.Z) {
			refuse(view.account, http.StatusConflict, "that player is not in view")
			return
		}
		label = opLabel(snap.UI.PlayerOps, a.Op)
		if label == "" {
			refuse(view.account, http.StatusBadRequest, "that player has no such option")
			return
		}
		label += " " + found.Name
		send = s.streamPlayerInteraction(&snap, found, func(ss actions.Session) error { return actions.OpPlayer(ss, a.Op, a.Index) })
	case "npc":
		var found *gamestate.NPC
		for i := range snap.NPCs {
			if snap.NPCs[i].Index == a.Index {
				found = &snap.NPCs[i]
			}
		}
		if found == nil {
			refuse(view.account, http.StatusConflict, "that npc is not in view")
			return
		}
		if names != nil {
			if t, ok := names.Npc(found.Type); ok {
				label = opLabel(t.Ops, a.Op)
			}
		}
		if label == "" {
			refuse(view.account, http.StatusBadRequest, "that npc has no such option")
			return
		}
		send = func(ss actions.Session) error { return actions.OpNpc(ss, a.Op, a.Index) }
	case "obj":
		present := false
		for _, o := range snap.Objs {
			if o.X == a.X && o.Z == a.Z && o.ID == a.ID && o.Level == snap.Level {
				present = true
			}
		}
		if !present {
			refuse(view.account, http.StatusConflict, "that ground item is not there")
			return
		}
		if names != nil {
			if t, ok := names.Obj(a.ID); ok {
				label = opLabel(t.Ops, a.Op)
				// The game client offers Take as option 3 for any item that leaves it unset.
				if label == "" && a.Op == 3 {
					label = "Take"
				}
			}
		}
		if label == "" {
			refuse(view.account, http.StatusBadRequest, "that item has no such option")
			return
		}
		send = func(ss actions.Session) error { return actions.OpObj(ss, a.Op, a.X, a.Z, a.ID) }
	case "loc":
		if !near(a.X, a.Z) {
			refuse(view.account, http.StatusBadRequest, "loc out of range")
			return
		}
		if names != nil {
			if t, ok := names.Loc(a.Type); ok {
				label = opLabel(t.Ops, a.Op)
			}
		}
		if label == "" {
			refuse(view.account, http.StatusBadRequest, "that loc has no such option")
			return
		}
		send = func(ss actions.Session) error { return actions.OpLoc(ss, a.Op, a.X, a.Z, a.Type) }
	default:
		refuse(view.account, http.StatusBadRequest, "unknown action kind")
		return
	}
	if err := s.src.(StreamSessionRunner).WithStreamSession(view.account, send); err != nil {
		s.auditStream(view.account, "action", detail+" "+label, "failed: "+err.Error())
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	s.auditStream(view.account, "action", detail+" "+label, "sent")
	writeJSON(w, http.StatusOK, struct {
		OK    bool   `json:"ok"`
		Label string `json:"label"`
	}{true, label})
}

// Native player clicks send the route before the interaction. This matters on engines
// using client-side routefinding: a distant Trade/Follow otherwise never approaches.
func (s *Server) streamPlayerInteraction(snap *gamestate.RenderSnapshot, target *gamestate.RenderPlayer, interact func(actions.Session) error) func(actions.Session) error {
	dx, dz := target.X-snap.X, target.Z-snap.Z
	if max(dx, -dx) <= 1 && max(dz, -dz) <= 1 {
		return interact
	}
	var pack *pathfind.Pack
	if ps, ok := s.src.(StreamPathSource); ok {
		pack = ps.StreamCollisionPack()
	}
	route, _ := streamWalkRoute(pack, pathfind.NavPoint{X: snap.X, Z: snap.Z, Level: snap.Level}, pathfind.NavPoint{X: target.X, Z: target.Z, Level: snap.Level})
	return func(ss actions.Session) error {
		if err := actions.Walk(ss, route, false); err != nil {
			return err
		}
		return interact(ss)
	}
}
