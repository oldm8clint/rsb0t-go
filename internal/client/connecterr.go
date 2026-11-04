package client

import "fmt"

// ConnectStage names the step of Connect that failed. Everything named here happens
// BEFORE the login server has given any verdict on this account — the cache checksums
// are fetched over plain HTTP and the WebSocket is dialled — so a failure at one of
// these stages says something about the server or the network between here and it, and
// nothing whatsoever about the account.
//
// That distinction is the whole reason this type exists. Live on 2026-09-20 a brief
// The live server outage left 31 of 269 accounts parked in the dashboard's terminal "error"
// state with no retry ever scheduled: 19 on `client: dial: websocket: bad handshake`
// (the HTTP layer was up and answered the WebSocket upgrade with something other than
// 101) and 12 on `client: checksums: expected >= 36 bytes from /crc0, got 0` (the same
// web server answering /crc0 with an empty body). Both were plain fmt.Errorf strings,
// so internal/fleet's retryableLoginError — which classifies by type — matched none of
// its cases, fell through to its default and gave up permanently on each account.
// Accounts that happened to fail a few milliseconds later in the same outage, at a
// stage that produces a typed error (a websocket.CloseError, a LoginRejectedError),
// all recovered on their own. The fix is to make these stages typed too rather than to
// pattern-match error text.
type ConnectStage string

const (
	// StageChecksums is FetchJagChecksums against HTTPBase+"crc0".
	StageChecksums ConnectStage = "checksums"
	// StageDial is the WebSocket dial and upgrade against WSURL.
	StageDial ConnectStage = "dial"
)

// ConnectError is a failure at one of the pre-login stages above. Its message is
// deliberately byte-identical to the fmt.Errorf wrapping it replaces ("client: dial:
// ..." / "client: checksums: ..."), so operator-facing logs, the dashboard's lastError
// field and the archived fleet logs all keep reading exactly as they did.
type ConnectError struct {
	Stage ConnectStage
	Err   error
}

func (e *ConnectError) Error() string { return fmt.Sprintf("client: %s: %v", e.Stage, e.Err) }

func (e *ConnectError) Unwrap() error { return e.Err }

// HTTPStatusError is a non-200 answer from one of the cache endpoints. It is separate
// from a body that is merely too short because the two want different words in the log:
// "got 0 bytes" reads like a client bug, "HTTP 502 Bad Gateway" reads like what it is.
//
// It also closes a real hole. fetchJagChecksumsWith used to ignore the status code
// entirely, so a 502 whose HTML error page happened to be 36 bytes or longer was parsed
// as nine archive CRCs and then CACHED for the full TTL — every account logging in
// during that window would have been rejected with response 6 ("out of date") by a
// server that was perfectly happy to admit them a moment later.
type HTTPStatusError struct {
	URL    string
	Status string
}

func (e *HTTPStatusError) Error() string { return fmt.Sprintf("%s: HTTP %s", e.URL, e.Status) }
