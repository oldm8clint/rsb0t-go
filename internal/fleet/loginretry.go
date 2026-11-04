package fleet

import (
	"errors"
	"io"
	"net"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
	"rsb0t-go/internal/client"
	"rsb0t-go/internal/proxydial"
	"rsb0t-go/internal/transport"
)

// Retry temporary server/transport failures, not bad credentials, bans, client
// version mismatches, malformed configuration or other unrecognised rejections.
func retryableLoginError(err error) bool {
	var rejected *client.LoginRejectedError
	if errors.As(err, &rejected) {
		switch rejected.Response {
		case 1, 5, 7, 8, 9, 10, 14, client.LoginAttemptsExceededResponse:
			return true
		case client.HopTimerResponse:
			// 21: "you logged out of another world too recently". Added 2026-09-22 with
			// multi-world support, and it was a real defect before that: the engine has
			// had this response on branch 289 all along (World.ts's onLoginMessage,
			// reply 10), it is the NORMAL answer to a world hop that happens inside the
			// 45-second hopTime window, and without this case it fell to the default and
			// was treated as TERMINAL — an account that hopped a few seconds too early
			// would have been retired permanently with "login failure requires
			// attention", exactly the shape of the 2026-09-20 outage this function's own
			// comment below describes.
			return true
		default:
			return false
		}
	}
	// A failure at one of client.Connect's pre-login stages — fetching /crc0, or dialling
	// and upgrading the WebSocket. The login server has not looked at this account yet, so
	// whatever went wrong is the server or the path to it, and both heal by themselves.
	//
	// This is the case the 2026-09-20 outage exposed, and it is worth being precise about
	// why the old code missed it. `websocket: bad handshake` is gorilla's plain
	// errors.New for "the upgrade request got a reply that was not 101" — an HTTP layer
	// that is up and answering 502/503 while the game server behind it restarts. It is not
	// a net.Error, not a CloseError, and wraps nothing, so every typed case below it fell
	// through to the default and 19 accounts were retired permanently. Twelve more went
	// the same way on an empty /crc0 body. Meanwhile accounts that reached the login
	// exchange a moment later and got a CloseError or a response 8 all recovered, which is
	// why the fleet appeared to "mostly" self-heal and the gap was easy to miss.
	//
	// Deliberate limitation, stated rather than papered over: a genuinely wrong WSURL or
	// HTTPBase now retries forever instead of failing once. Nothing observable at this
	// stage distinguishes "the address is wrong" from "the server is down" — the server
	// being down is the case that happens in production and the one worth healing, and a
	// wrong address costs one attempt per account per reconnectBackoffMax while its error
	// text sits on the dashboard the entire time.
	var connecting *client.ConnectError
	if errors.As(err, &connecting) {
		return true
	}
	// A proxy route failing (routes.go) is temporary for the account: the route goes into
	// backoff and the account fails over to another route or waits in the queue.
	var proxyErr *proxydial.Error
	if errors.As(err, &proxyErr) {
		return true
	}
	for _, transient := range []error{transport.ErrReadTimeout, transport.ErrClosed, io.EOF, io.ErrUnexpectedEOF,
		syscall.ECONNREFUSED, syscall.ECONNRESET, syscall.ETIMEDOUT, syscall.EPIPE, syscall.ENETUNREACH, syscall.EHOSTUNREACH} {
		if errors.Is(err, transient) {
			return true
		}
	}
	for _, transient := range platformTransientLoginErrors {
		if errors.Is(err, transient) {
			return true
		}
	}
	var ws *websocket.CloseError
	if errors.As(err, &ws) {
		return ws.Code == websocket.CloseNormalClosure || ws.Code == websocket.CloseGoingAway || ws.Code == websocket.CloseAbnormalClosure
	}
	var network net.Error
	return errors.As(err, &network) && (network.Timeout() || network.Temporary())
}

func waitLoginRetry(done <-chan struct{}, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-done:
		return false
	case <-timer.C:
		return true
	}
}

// WaitConnected lets startup callers wait through temporary login retries before
// starting their configured script. It ends on success, a terminal failure,
// deliberate disconnect, unknown account, or manager shutdown.
func (m *Manager) WaitConnected(username string) bool {
	a := m.find(username)
	if a == nil {
		return false
	}
	for {
		select {
		case <-m.stop:
			return false
		default:
		}
		a.mu.RLock()
		connected := a.session != nil && !a.disconnected
		stopped := a.deliberateDisconnect || (!a.connecting && !a.reconnecting && a.lastErr != "")
		a.mu.RUnlock()
		if stopped {
			return false
		}
		if connected {
			return true
		}
		if !waitLoginRetry(m.stop, 100*time.Millisecond) {
			return false
		}
	}
}
