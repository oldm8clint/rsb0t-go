// Package serverprobe turns the address a person types for a game server into the endpoints the
// client needs, and checks, read-only, that a RuneScape 2 revision-289 server built on the LostCity
// engine answers there: the web server is reachable, it serves the nine archive checksums at /crc0,
// its web client carries an RSA login key, and its game WebSocket opens. Nothing here logs in, sends a
// login block or a password, or creates anything on the server.
//
// What a LostCity Engine-TS 289 web server serves, read from the engine's own src/web.ts: the game
// WebSocket on "/" (the same port as HTTP), the checksum table at /crc<anything>, and the web client
// under /client/ (client.js carries the login key the server decrypts with). There is no /loginkey
// route. A server that changed its revision still passes these checks, because nothing it serves
// before a login states the revision; only the first login says so (response 6), and the report says
// that rather than claiming more than it measured.
package serverprobe

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"rsb0t-go/internal/fleet"
)

// Endpoints are the two addresses a login needs: the game WebSocket and the HTTP base (ending in "/").
type Endpoints struct {
	WSURL    string `json:"wsURL"`
	HTTPBase string `json:"httpBase"`
}

// Normalize turns what a person typed (example-server.com, https://example-server.com/play,
// wss://w1.example-server.com/, localhost:8888) into endpoints. A bare name gets https, except a
// local or test name (localhost, an IP address, *.localhost, *.test), which gets http. A web path is
// dropped, since the game is served from "/"; a ws:// or wss:// path is kept for the WebSocket. A bare
// IPv6 address (::1, fd00::5) is put in brackets.
func Normalize(addr string) (Endpoints, error) {
	s := strings.TrimSpace(addr)
	if s == "" {
		return Endpoints{}, errors.New("the server address is empty")
	}
	if !strings.Contains(s, "://") {
		host := s
		if i := strings.IndexAny(host, "/?#"); i >= 0 {
			host = host[:i]
		}
		if ip := net.ParseIP(host); ip != nil && strings.Contains(host, ":") {
			// A bare IPv6 address: its colons are not a port.
			s = "[" + host + "]" + s[len(host):]
			host = "[" + host + "]"
		}
		if isLocalHost(hostOnly(host)) || net.ParseIP(hostOnly(host)) != nil {
			// A server reached by its IP address almost never has a certificate for it, so plain http.
			s = "http://" + s
		} else {
			s = "https://" + s
		}
	}
	u, err := url.Parse(s)
	if err != nil {
		return Endpoints{}, fmt.Errorf("%q is not a web address: %v", addr, err)
	}
	if u.User != nil {
		return Endpoints{}, fmt.Errorf("%q has a user name or password in it; give just the address", addr)
	}
	if u.Host == "" || u.Hostname() == "" {
		return Endpoints{}, fmt.Errorf("%q has no host name in it; give an address like https://example-server.com", addr)
	}
	if strings.ContainsAny(u.Hostname(), " \t") {
		return Endpoints{}, fmt.Errorf("%q has a space in its host name", addr)
	}
	var web, game, wsPath string
	switch strings.ToLower(u.Scheme) {
	case "https":
		web, game = "https", "wss"
	case "http":
		web, game = "http", "ws"
	case "wss":
		web, game, wsPath = "https", "wss", u.EscapedPath()
	case "ws":
		web, game, wsPath = "http", "ws", u.EscapedPath()
	default:
		return Endpoints{}, fmt.Errorf("%q starts with %s://; use https://, http://, wss:// or ws://", addr, u.Scheme)
	}
	if wsPath == "" {
		wsPath = "/"
	}
	host := strings.ToLower(u.Host)
	return Endpoints{WSURL: game + "://" + host + wsPath, HTTPBase: web + "://" + host + "/"}, nil
}

// GameEndpoint turns a separately given game connection address (wss://example-server.com:43595/game,
// or https://... which becomes wss://...) into a WebSocket URL, for a server whose game is not on its
// website's host and port.
func GameEndpoint(addr string) (string, error) {
	s := strings.TrimSpace(addr)
	if !strings.Contains(s, "://") {
		s = "wss://" + s
	}
	u, err := url.Parse(s)
	if err != nil || u.Hostname() == "" {
		return "", fmt.Errorf("%q is not a game connection address; give one like wss://example-server.com:43595/", addr)
	}
	switch strings.ToLower(u.Scheme) {
	case "wss", "https":
		u.Scheme = "wss"
	case "ws", "http":
		u.Scheme = "ws"
	default:
		return "", fmt.Errorf("%q starts with %s://; a game connection address starts with wss:// or ws://", addr, u.Scheme)
	}
	if u.User != nil {
		return "", fmt.Errorf("%q has a user name or password in it; give just the address", addr)
	}
	u.Host = strings.ToLower(u.Host)
	if u.Path == "" {
		u.Path = "/"
	}
	u.RawQuery, u.Fragment = "", ""
	return u.String(), nil
}

// IsLocal is whether a URL's host is this machine, a private network address or a test name: a server
// the person runs themselves, not a public one.
func IsLocal(raw string) bool {
	h := hostOf(raw)
	return h != "" && isLocalHost(h)
}

// hostOf is the host name of a URL, "" when it has none.
func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

func hostOnly(hostport string) string {
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		return h
	}
	return strings.Trim(hostport, "[]")
}

// isLocalHost is whether a host name is this machine, a private network address or a test name.
func isLocalHost(h string) bool {
	h = strings.ToLower(strings.TrimSuffix(h, "."))
	if h == "localhost" || strings.HasSuffix(h, ".localhost") || strings.HasSuffix(h, ".test") {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast())
}

// Status is one check's outcome.
type Status string

const (
	OK   Status = "ok"
	Warn Status = "warn"
	Fail Status = "fail"
	Skip Status = "skipped"
)

// Step is one check, in words a person can act on.
type Step struct {
	Name   string `json:"name"`
	Status Status `json:"status"`
	Detail string `json:"detail"`
}

// Report is what Check found. Ready is true when nothing failed: rsb0t has everything it needs to try
// a login there.
type Report struct {
	Address   string    `json:"address"`
	Endpoints Endpoints `json:"endpoints"`
	Ready     bool      `json:"ready"`
	Verdict   string    `json:"verdict"`
	Steps     []Step    `json:"steps"`

	// Modulus and Exponent are the login key (decimal), found or pasted. KeyPasted says which.
	Modulus   string `json:"modulus,omitempty"`
	Exponent  string `json:"exponent,omitempty"`
	KeyFound  bool   `json:"keyFound"`
	KeyPasted bool   `json:"keyPasted"`
	// KeyPastedBad is true when a key was pasted and could not be used.
	KeyPastedBad bool `json:"keyPastedBad"`
	// GameFilesOK is true when /crc0 answered, so the address is a LostCity server's.
	GameFilesOK bool `json:"gameFilesOK"`
	// GameURLGiven is true when the game connection address was given separately (Options.GameURL).
	GameURLGiven bool `json:"gameURLGiven"`
	// Worlds are the world numbers that answered, when the host names a world (w1.example-server.com).
	Worlds []int `json:"worlds,omitempty"`
	// NeedsBrowserHeaders is true when the game WebSocket opened only with an Origin header.
	NeedsBrowserHeaders bool `json:"needsBrowserHeaders"`
}

// Options adjust a check. The zero value is right for a real check.
type Options struct {
	// Modulus and Exponent are a key the person pasted (decimal; exponent empty means 65537). When
	// set, the client bundle is not searched.
	Modulus, Exponent string
	// GameURL, when set, is the game connection address, for a server whose game WebSocket is not on its
	// website's host and port (see GameEndpoint).
	GameURL string
	// MaxWorlds is how many wN. hosts are tried when the address names a world (default 10).
	MaxWorlds int
	// Timeout bounds each request (default 10s).
	Timeout time.Duration
	// DialContext replaces the network dialer for HTTP and the WebSocket (tests point names at a
	// local server with it).
	DialContext func(ctx context.Context, network, addr string) (net.Conn, error)
}

func (o Options) httpClient() *http.Client {
	t := http.DefaultTransport.(*http.Transport).Clone()
	if o.DialContext != nil {
		t.DialContext = o.DialContext
	}
	return &http.Client{Timeout: o.timeout(), Transport: t}
}

func (o Options) timeout() time.Duration {
	if o.Timeout > 0 {
		return o.Timeout
	}
	return 10 * time.Second
}

var reWorldHost = regexp.MustCompile(`^w([0-9]+)\.`)

// Check runs the read-only checks against addr and says, in plain English, what it found.
func Check(ctx context.Context, addr string, opt Options) Report {
	rep := Report{Address: strings.TrimSpace(addr)}
	ep, err := Normalize(addr)
	if err != nil {
		rep.Steps = append(rep.Steps, Step{"Address", Fail, err.Error()})
		rep.Verdict = "That is not a server address rsb0t can use: " + err.Error() + "."
		return rep
	}
	if strings.TrimSpace(opt.GameURL) != "" {
		ws, err := GameEndpoint(opt.GameURL)
		if err != nil {
			rep.Steps = append(rep.Steps, Step{"Game connection address", Fail, err.Error()})
			rep.Verdict = "That is not a game connection address rsb0t can use: " + err.Error() + "."
			return rep
		}
		ep.WSURL, rep.GameURLGiven = ws, true
	}
	rep.Endpoints = ep
	hc := opt.httpClient()

	// 1. Is anything there at all?
	if err := reachable(ctx, hc, ep.HTTPBase); err != nil {
		rep.Steps = append(rep.Steps, Step{"Reachable", Fail, fmt.Sprintf("Nothing answered at %s: %s. Check the address is spelt right, that the server is online, and that this computer has internet access.", ep.HTTPBase, plainNetError(err))})
		rep.Verdict = "rsb0t could not reach " + ep.HTTPBase + ". Check the address and that the server is online."
		return rep
	}
	rep.Steps = append(rep.Steps, Step{"Reachable", OK, ep.HTTPBase + " answers."})

	// 2. The checksum table every LostCity server publishes, and every login carries.
	final, crcErr := fetchCRC(ctx, hc, ep.HTTPBase)
	if crcErr != nil && !reWorldHost.MatchString(hostOf(ep.HTTPBase)) {
		// The website and the game are often on different hosts: example-server.com (or
		// www.example-server.com) and w1.example-server.com. Try world 1 before giving up.
		w1 := func(u string) string {
			return strings.Replace(strings.Replace(u, "://www.", "://", 1), "://", "://w1.", 1)
		}
		if alt, err := Normalize(w1(ep.HTTPBase)); err == nil {
			if reachable(ctx, hc, alt.HTTPBase) == nil {
				if f, err := fetchCRC(ctx, hc, alt.HTTPBase); err == nil {
					if rep.GameURLGiven {
						alt.WSURL = ep.WSURL
					} else {
						alt.WSURL = w1(ep.WSURL)
					}
					ep, final, crcErr = alt, f, nil
					rep.Endpoints = ep
					rep.Steps = append(rep.Steps, Step{"Game host", OK, "The website has no game files, but " + ep.HTTPBase + " does, so rsb0t uses that."})
				}
			}
		}
	}
	if crcErr == nil && final != "" && final != ep.HTTPBase {
		// The server sent /crc0 on to another address (usually http to https): use that address, and its
		// matching game connection, rather than one that only works through the redirect.
		if moved, err := Normalize(final); err == nil && moved.HTTPBase != ep.HTTPBase {
			if !rep.GameURLGiven {
				moved.WSURL = swapOrigin(ep.WSURL, moved.WSURL)
			} else {
				moved.WSURL = ep.WSURL
			}
			rep.Steps = append(rep.Steps, Step{"Redirect", OK, ep.HTTPBase + " sends its game files on to " + moved.HTTPBase + ", so rsb0t uses that address."})
			ep = moved
			rep.Endpoints = ep
		}
	}
	if crcErr != nil {
		rep.Steps = append(rep.Steps, Step{"Game files (/crc0)", Fail, crcErr.Error() + ". A LostCity revision-289 server serves its nine archive checksums at /crc0; this address does not, so it is not one, or the game is on another address (try the address of the page where you play, or a world address such as w1.<the site>)."})
		rep.Verdict = "This does not look like a LostCity revision-289 game server: " + ep.HTTPBase + "crc0 is missing or too short."
		return rep
	}
	rep.GameFilesOK = true
	rep.Steps = append(rep.Steps, Step{"Game files (/crc0)", OK, "The server publishes its checksum table, as a LostCity server does."})

	// 3. The login key.
	keyOK := true
	pastedWhy := ""
	if strings.TrimSpace(opt.Modulus) != "" || strings.TrimSpace(opt.Exponent) != "" {
		mod, exp, err := ParseKey(opt.Modulus, opt.Exponent)
		if err == nil {
			rep.Modulus, rep.Exponent, rep.KeyPasted = mod.String(), exp.String(), true
			rep.Steps = append(rep.Steps, Step{"Login key", OK, fmt.Sprintf("Using the key you pasted (a %d-digit modulus).", len(rep.Modulus))})
		} else {
			rep.KeyPastedBad = true
			pastedWhy = err.Error()
		}
	}
	if rep.KeyPasted {
		// done
	} else if mod, exp, err := fleet.FetchLoginKeyWith(hc, ep.HTTPBase); err == nil && rep.KeyPastedBad {
		rep.Modulus, rep.Exponent, rep.KeyFound = mod.String(), exp.String(), true
		rep.Steps = append(rep.Steps, Step{"Login key", Warn, "The key you pasted cannot be used (" + pastedWhy + "), but rsb0t found the server's key in its web client by itself. Clear the two key boxes (or leave out -modulus and -exponent) and it uses that one."})
	} else if rep.KeyPastedBad {
		keyOK = false
		rep.Steps = append(rep.Steps, Step{"Login key", Fail, "The key you pasted cannot be used: " + pastedWhy + "."})
	} else if err != nil {
		keyOK = false
		why := plainNetError(err)
		if errors.Is(err, fleet.ErrNoLoginKey) {
			why = "the server's web client (" + ep.HTTPBase + "client/client.js) has no login key rsb0t can recognise"
		}
		rep.Steps = append(rep.Steps, Step{"Login key", Fail, why + ". " + PasteKeyHelp(ep.HTTPBase)})
	} else {
		rep.Modulus, rep.Exponent, rep.KeyFound = mod.String(), exp.String(), true
		rep.Steps = append(rep.Steps, Step{"Login key", OK, fmt.Sprintf("Found automatically in the web client (a %d-digit modulus).", len(rep.Modulus))})
	}

	// 4. The game connection itself: open the WebSocket and close it, sending nothing.
	gameOK, gameNotWS := true, false
	if err := openGame(ctx, opt, ep, false); err == nil {
		rep.Steps = append(rep.Steps, Step{"Game connection", OK, ep.WSURL + " opens."})
	} else if err2 := openGame(ctx, opt, ep, true); err2 == nil {
		rep.NeedsBrowserHeaders = true
		rep.Steps = append(rep.Steps, Step{"Game connection", Warn, ep.WSURL + " opens only for a browser (it checks the Origin header), so rsb0t will send browser headers."})
	} else {
		gameOK = false
		var hs *handshakeStatus
		gameNotWS = errors.As(err, &hs)
		rep.Steps = append(rep.Steps, Step{"Game connection", Fail, fmt.Sprintf("%s would not open: %s.", ep.WSURL, plainNetError(err))})
	}

	// 5. Worlds.
	if m := reWorldHost.FindStringSubmatch(hostOf(ep.HTTPBase)); m != nil {
		rep.Worlds = discoverWorlds(ctx, hc, ep, opt.MaxWorlds)
		if len(rep.Worlds) > 1 {
			rep.Steps = append(rep.Steps, Step{"Worlds", OK, "Worlds that answer: " + joinInts(rep.Worlds) + ". Accounts start on world " + m[1] + " unless you give one a world."})
		} else {
			rep.Steps = append(rep.Steps, Step{"Worlds", OK, "One world answers (world " + m[1] + ")."})
		}
	} else {
		rep.Steps = append(rep.Steps, Step{"Worlds", OK, "One world: this address. If the server has more, give the address of one world (like w1.<the site>) and rsb0t finds the others."})
	}

	rep.Ready = keyOK && gameOK
	switch {
	case rep.Ready:
		rep.Verdict = "Ready. rsb0t can reach this server, it looks like a LostCity revision-289 server, and its login key is known. " +
			"The revision is only confirmed by the first login: if every login is refused with response 6, the server runs a different revision and this client will not work there."
	case rep.KeyPastedBad && !keyOK:
		rep.Verdict = "The login key you pasted cannot be used: " + pastedWhy + ". Check you copied the whole of both numbers, or clear the two key boxes to let rsb0t look for the key itself."
	case !keyOK:
		rep.Verdict = "Almost: the server is there, but rsb0t needs its login key. " + PasteKeyHelp(ep.HTTPBase)
	case gameNotWS:
		rep.Verdict = "The server's web pages answer, but " + ep.WSURL + " is not its game connection (the web server answered there instead of the game). " +
			"Some servers put the game on another port or path: ask the server's staff for the game connection address and give it as well " +
			"(the \"Game connection address\" box on the setup page, or -game <address> on the command line)."
	default:
		rep.Verdict = "The server's web pages answer, but its game connection does not open, so accounts would not connect. It may be restarting; try again in a few minutes."
	}
	return rep
}

// PasteKeyHelp says how to find a server's login key by hand.
func PasteKeyHelp(httpBase string) string {
	return "The easiest way: ask the server's staff for its login public key (two numbers) and paste them into the two key boxes " +
		"(or run `rsb0t server <address> -modulus <first number> -exponent <second number>`). " +
		"To find it yourself: open " + httpBase + "client/client.js in your browser, press Ctrl+F (Cmd+F on a Mac) and search for BigInt(; " +
		"the first very long number in quotes after it is the first number, and the number in the next BigInt( is the second."
}

// ParseKey checks a pasted key: decimal digits only (spaces, quotes and line breaks are removed), a
// modulus of at least 100 digits, and an exponent that defaults to 65537.
func ParseKey(modulus, exponent string) (mod, exp *big.Int, err error) {
	clean := func(s string) string {
		return strings.Map(func(r rune) rune {
			if r == ' ' || r == '\n' || r == '\r' || r == '\t' || r == '"' || r == '\'' || r == ',' {
				return -1
			}
			return r
		}, strings.TrimSpace(s))
	}
	m, e := clean(modulus), clean(exponent)
	if e == "" {
		e = "65537"
	}
	mod, ok := new(big.Int).SetString(m, 10)
	if !ok || mod.Sign() <= 0 {
		return nil, nil, errors.New("the modulus must be a whole number written in digits 0-9")
	}
	if len(m) < 100 {
		return nil, nil, fmt.Errorf("the modulus is only %d digits; a login key's modulus is a much longer number (150 digits or more)", len(m))
	}
	exp, ok = new(big.Int).SetString(e, 10)
	if !ok || exp.Sign() <= 0 {
		return nil, nil, errors.New("the exponent must be a whole number written in digits 0-9")
	}
	return mod, exp, nil
}

func reachable(ctx context.Context, hc *http.Client, base string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base, nil)
	if err != nil {
		return err
	}
	resp, err := noRedirect(hc).Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// noRedirect is hc not following redirects: any answer at all means the host is there.
func noRedirect(hc *http.Client) *http.Client {
	c := *hc
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &c
}

// fetchCRC reads base's /crc0 and returns the base it was finally served from (after any redirect).
func fetchCRC(ctx context.Context, hc *http.Client, base string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"crc0", nil)
	if err != nil {
		return "", err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return "", fmt.Errorf("%scrc0 could not be read: %s", base, plainNetError(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%scrc0 answered HTTP %d", base, resp.StatusCode)
	}
	var buf [36]byte
	n, _ := io.ReadFull(resp.Body, buf[:])
	if n < len(buf) {
		return "", fmt.Errorf("%scrc0 returned %d bytes, where a LostCity server returns at least 36", base, n)
	}
	final := base
	if resp.Request != nil && resp.Request.URL != nil {
		u := *resp.Request.URL
		u.Path, u.RawPath, u.RawQuery, u.Fragment = "/", "", "", ""
		final = u.String()
	}
	return final, nil
}

func openGame(ctx context.Context, opt Options, ep Endpoints, withOrigin bool) error {
	d := websocket.Dialer{HandshakeTimeout: opt.timeout(), Proxy: http.ProxyFromEnvironment}
	if opt.DialContext != nil {
		d.NetDialContext = opt.DialContext
	}
	var hdr http.Header
	if withOrigin {
		u, err := url.Parse(ep.HTTPBase)
		if err != nil {
			return err
		}
		hdr = http.Header{"Origin": {u.Scheme + "://" + u.Host}}
	}
	ctx, cancel := context.WithTimeout(ctx, opt.timeout())
	defer cancel()
	c, resp, err := d.DialContext(ctx, ep.WSURL, hdr)
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	if err != nil {
		if resp != nil {
			return &handshakeStatus{resp.StatusCode}
		}
		return err
	}
	return c.Close()
}

// handshakeStatus is a game connection the web server answered with a page instead of opening.
type handshakeStatus struct{ code int }

func (h *handshakeStatus) Error() string {
	return fmt.Sprintf("the web server answered there (HTTP %d) instead of opening a game connection", h.code)
}

// swapOrigin keeps ws's path and puts it on moved's scheme and host.
func swapOrigin(ws, moved string) string {
	a, err1 := url.Parse(ws)
	b, err2 := url.Parse(moved)
	if err1 != nil || err2 != nil {
		return moved
	}
	a.Scheme, a.Host = b.Scheme, b.Host
	return a.String()
}

func discoverWorlds(ctx context.Context, hc *http.Client, ep Endpoints, max int) []int {
	if max <= 0 {
		max = 10
	}
	var (
		mu  sync.Mutex
		out []int
		wg  sync.WaitGroup
	)
	for n := 1; n <= max; n++ {
		base := reWorldHostInURL.ReplaceAllString(ep.HTTPBase, "${1}w"+strconv.Itoa(n)+".")
		wg.Add(1)
		go func(n int, base string) {
			defer wg.Done()
			if _, err := fetchCRC(ctx, hc, base); err == nil {
				mu.Lock()
				out = append(out, n)
				mu.Unlock()
			}
		}(n, base)
	}
	wg.Wait()
	sort.Ints(out)
	return out
}

var reWorldHostInURL = regexp.MustCompile(`^(https?://)w[0-9]+\.`)

func joinInts(ns []int) string {
	s := make([]string, len(ns))
	for i, n := range ns {
		s[i] = strconv.Itoa(n)
	}
	return strings.Join(s, ", ")
}

// plainNetError turns a network error into a short reason a person can act on.
func plainNetError(err error) string {
	if err == nil {
		return ""
	}
	var hs *handshakeStatus
	if errors.As(err, &hs) {
		return hs.Error()
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return "there is no server with that name (the name does not resolve)"
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) && opErr.Op == "dial" {
		if strings.Contains(strings.ToLower(opErr.Err.Error()), "refused") {
			return "the computer at that address refused the connection (nothing is listening on that port)"
		}
	}
	if errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "Client.Timeout") || strings.Contains(err.Error(), "timeout") {
		return "it did not answer in time"
	}
	s := strings.ToLower(err.Error())
	if strings.Contains(s, "certificate") || strings.Contains(s, "x509") || strings.Contains(s, "tls") {
		return "its secure (https) connection failed; if the server has no https, try the address with http:// in front"
	}
	if strings.Contains(s, "refused") {
		return "the connection was refused (nothing is listening there)"
	}
	return err.Error()
}
