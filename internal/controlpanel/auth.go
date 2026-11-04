package controlpanel

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net"
	"net/http"
	"net/url"
	"strings"
)

const cookieName = "rsb0t_dashboard"

// img-src additionally allows https://example.invalid: the account map panel (web/panels/map.js)
// draws real world-map overview tiles fetched live from example.invalid's own public /deaths map
// (world-overview-surface.webp/world-overview-underground.webp) as its background, per the
// account owner's explicit request for accurate map art instead of a bare coordinate plot. This
// is a real, live, external image dependency by design (see that panel's own comments) — if
// example.invalid ever moves those paths, the map's background silently falls back to its old flat
// plot rather than failing, but this CSP line is what makes fetching it possible at all.
const contentSecurityPolicy = "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data: https://example.invalid; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'"

// guard is the one gate every request passes: security headers, the Host check, the cross-origin
// check on writes, and the token.
type guard struct {
	loopbackOnly bool
	hasToken     bool
	tokenHash    [32]byte
	// dashboardBlobImages lets the dashboard page itself load blob: images. Set only when
	// a stream client provider AND the capture adapter are installed: the cache-terrain
	// provider runs the capture adapter inside the dashboard page, and that adapter's
	// cache decoder turns sprite data into blob: images (exactly why /capture.html already
	// gets blob:). Without it the decoder waits forever on an image the CSP blocked.
	dashboardBlobImages bool
}

func newGuard(cfg Config) guard {
	host, _, _ := net.SplitHostPort(cfg.Listen)
	// cfg.AllowRemoteHost (see its own doc comment on Config, and normalised's enforcement
	// that it never lands here without a Token) is this guard's one deliberate way to relax
	// the loopback Host-header check below: an operator opting into a tunnel already proved
	// they configured a Token by the time this runs, and the token check further down in
	// wrap gates every request regardless of Host once hasToken is true — so this trades one
	// defense-in-depth layer for the ability to actually be reached through a tunnel, only
	// when a real auth boundary (the token) is already in place to replace it.
	g := guard{loopbackOnly: isLoopbackName(host) && !cfg.AllowRemoteHost}
	g.dashboardBlobImages = cfg.StreamClientBundle != "" && cfg.CaptureRenderer != ""
	if cfg.Token != "" {
		g.hasToken = true
		g.tokenHash = sha256.Sum256([]byte(cfg.Token))
	}
	return g
}

// matches compares hashes, so neither the time taken nor an early length mismatch says anything
// about the token.
func (g guard) matches(candidate string) bool {
	h := sha256.Sum256([]byte(candidate))
	return subtle.ConstantTimeCompare(h[:], g.tokenHash[:]) == 1
}

// cookieValue is the token's hash rather than the token, so a copied cookie does not reveal a
// token that may be reused elsewhere.
func (g guard) cookieValue() string {
	return hex.EncodeToString(g.tokenHash[:])
}

func (g guard) authorised(r *http.Request) bool {
	if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") && g.matches(strings.TrimPrefix(auth, "Bearer ")) {
		return true
	}
	c, err := r.Cookie(cookieName)
	return err == nil && subtle.ConstantTimeCompare([]byte(c.Value), []byte(g.cookieValue())) == 1
}

func (g guard) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "DENY")
		policy := contentSecurityPolicy
		if r.URL.Path == "/capture.html" || (g.dashboardBlobImages && (r.URL.Path == "/" || r.URL.Path == "/index.html" || r.URL.Path == "/stream/view")) {
			policy = strings.Replace(policy, "img-src 'self' data:", "img-src 'self' data: blob:", 1)
		}
		h.Set("Content-Security-Policy", policy)

		// Why: without this a page on any website can rebind its own hostname to 127.0.0.1 and
		// read the fleet through the browser.
		if g.loopbackOnly && !loopbackHostHeader(r.Host) {
			refuse(w, r, http.StatusForbidden, "this dashboard answers only to localhost, 127.0.0.1 or [::1]")
			return
		}
		if !safeMethod(r.Method) && !sameOrigin(r) {
			refuse(w, r, http.StatusForbidden, "cross-origin request refused")
			return
		}
		if g.hasToken {
			if token := r.URL.Query().Get("token"); token != "" && r.Method == http.MethodGet {
				if !g.matches(token) {
					refuse(w, r, http.StatusUnauthorized, "wrong token")
					return
				}
				// Why: EventSource cannot send a header, so the token becomes a cookie once and
				// leaves the address bar and the history.
				http.SetCookie(w, &http.Cookie{Name: cookieName, Value: g.cookieValue(), Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
				u := *r.URL
				q := u.Query()
				q.Del("token")
				u.RawQuery = q.Encode()
				http.Redirect(w, r, u.RequestURI(), http.StatusSeeOther)
				return
			}
			if !g.authorised(r) {
				refuse(w, r, http.StatusUnauthorized, "unauthorized: open the dashboard once as /?token=<token>, or send Authorization: Bearer <token>")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func refuse(w http.ResponseWriter, r *http.Request, code int, msg string) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		writeError(w, code, msg)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_, _ = w.Write([]byte(msg + "\n"))
}

func safeMethod(m string) bool {
	return m == http.MethodGet || m == http.MethodHead || m == http.MethodOptions
}

func loopbackHostHeader(hostport string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	return isLoopbackName(host)
}

// sameOrigin is true for a request with no Origin (curl, a test) or one from this page.
func sameOrigin(r *http.Request) bool {
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	return err == nil && strings.EqualFold(u.Host, r.Host)
}
