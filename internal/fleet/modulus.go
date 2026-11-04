package fleet

import (
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"regexp"
	"time"

	"rsb0t-go/internal/client"
)

// modulusPattern matches a 250+ digit decimal run, the same heuristic
// loginKey.ts's extractLoginModulus uses against the minified client bundle.
var modulusPattern = regexp.MustCompile(`\d{250,}`)

// keyPairPattern matches the call the web client makes to install its login key: two quoted
// decimal BigInts side by side, modulus then exponent, as in a minified LostCity client's
// `.Ka(BigInt("<modulus>"),BigInt("<exponent>"))`. A stock Engine-TS 289 checkout ships a 512-bit
// key (a 154-digit modulus) with a 77-digit exponent, which the 250+ digit rule alone misses and
// for which 65537 would be the wrong exponent.
var keyPairPattern = regexp.MustCompile(`BigInt\(\s*["'](\d{100,})["']\s*\)\s*,\s*BigInt\(\s*["'](\d{1,400})["']\s*\)`)

// defaultExponent is the public exponent taken for a bundle that states only a modulus.
const defaultExponent = 65537

// ErrNoLoginKey is wrapped by ExtractLoginKey and FetchLoginKey when the bundle holds no key.
var ErrNoLoginKey = errors.New("no RSA login key found in the client bundle")

// keyFetchTimeout bounds one fetch of the client bundle, so a server that accepts the connection
// and never answers is an error rather than a hang.
const keyFetchTimeout = 30 * time.Second

// ExtractLoginKey finds the RSA login key in a web client bundle. A 250+ digit run is taken first,
// exactly as before (its exponent is read from the BigInt pair that carries it when there is one,
// else 65537); failing that, the first BigInt(modulus),BigInt(exponent) pair with a modulus of 100
// digits or more is taken, which is how a stock Engine-TS 289 bundle carries its key.
func ExtractLoginKey(body []byte) (mod, exp *big.Int, err error) {
	parse := func(s string) *big.Int {
		v, ok := new(big.Int).SetString(s, 10)
		if !ok || v.Sign() <= 0 {
			return nil
		}
		return v
	}
	pairs := keyPairPattern.FindAllSubmatch(body, -1)
	if m := modulusPattern.Find(body); m != nil {
		mod = parse(string(m))
		if mod == nil {
			return nil, nil, fmt.Errorf("fleet: matched text is not a valid integer: %.40s...", m)
		}
		exp = big.NewInt(defaultExponent)
		for _, p := range pairs {
			if string(p[1]) == string(m) {
				if e := parse(string(p[2])); e != nil {
					exp = e
				}
				break
			}
		}
		return mod, exp, nil
	}
	for _, p := range pairs {
		mod, exp = parse(string(p[1])), parse(string(p[2]))
		if mod != nil && exp != nil {
			return mod, exp, nil
		}
	}
	return nil, nil, fmt.Errorf("fleet: %w", ErrNoLoginKey)
}

// FetchLoginKey reads httpBase+"client/client.js" and extracts the login key from it (see
// ExtractLoginKey). httpBase must end in "/".
func FetchLoginKey(httpBase string) (mod, exp *big.Int, err error) {
	return FetchLoginKeyWith(&http.Client{Timeout: keyFetchTimeout}, httpBase)
}

// FetchLoginKeyWith is FetchLoginKey with the HTTP client given.
func FetchLoginKeyWith(hc *http.Client, httpBase string) (mod, exp *big.Int, err error) {
	resp, err := hc.Get(httpBase + "client/client.js")
	if err != nil {
		return nil, nil, fmt.Errorf("fleet: fetching client bundle: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("fleet: %sclient/client.js answered HTTP %d: %w", httpBase, resp.StatusCode, ErrNoLoginKey)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return nil, nil, fmt.Errorf("fleet: reading client bundle: %w", err)
	}
	return ExtractLoginKey(body)
}

// FetchProductionModulus scrapes the live RSA login modulus out of the production
// client's own JS bundle at httpBase+"client/client.js" — the same fallback
// rsb0t-main's loginKey.ts uses, since the live server rotates its keypair on server restart
// and never exposes a dedicated /loginkey endpoint on this deployment (confirmed: it
// 404s both on the marketing site and on w1.example.invalid). FetchLoginKey also returns
// the exponent.
func FetchProductionModulus(httpBase string) (*big.Int, error) {
	mod, _, err := FetchLoginKey(httpBase)
	return mod, err
}

// ---------------------------------------------------------------------------
// Recovering from a rotated login key
// ---------------------------------------------------------------------------
//
// The live server generates a fresh RSA keypair every time the server process restarts (see
// FetchProductionModulus above and CLAUDE.md's "The production key" gotcha), and the
// fleet scrapes the modulus exactly once, at startup. Until now nothing ever refreshed
// it, so a server restart left every account in the process signing its login block with
// a key the server no longer holds.
//
// What that looks like from here is worth spelling out, because it is NOT obvious and it
// is indistinguishable from two other things. The engine decrypts the RSA block and
// checks that the first plaintext byte is 10; when it is not, it answers **response 6**
// and comments the line "sending out of date intentionally" (engine World.ts). Response
// 6 is also what it answers for a client revision mismatch, and for a cache CRC
// mismatch. retryableLoginError treats 6 as terminal — correctly, for a revision
// mismatch, which no amount of retrying fixes.
//
// So a key rotation and a permanently out-of-date client produce byte-identical
// rejections, and the fleet must not guess. The rule here is therefore evidence-based
// rather than heuristic: on a response 6, go and look at the server's current modulus,
// and treat the rejection as retryable only if the key has ACTUALLY changed since the
// attempt was made. A revision mismatch still stops, because the key will still match.

// modulusRecheckInterval bounds how often a response 6 can trigger a real HTTP fetch of
// the client bundle. Generation tracking (see staleLoginKey) already collapses a whole
// cohort rejected by one rotation into a single fetch; this bounds the other case, where
// the key has not changed and every account is genuinely out of date, to one fetch per
// interval for the whole fleet rather than one per account.
const modulusRecheckInterval = 15 * time.Second

// loginKey returns the modulus every login should currently be signed with, and the
// generation it belongs to. Generation 0 is whatever Config.Modulus was built with; it
// increases by one each time a rotation is observed.
func (m *Manager) loginKey() (*big.Int, int) {
	m.modMu.Lock()
	defer m.modMu.Unlock()
	if m.modulus == nil {
		m.modulus = m.cfg.Modulus
	}
	return m.modulus, m.modGen
}

// staleLoginKey reports whether err is a rejection that a key rotation explains, and the
// rotation has been confirmed against the server. usedGen is the generation the failed
// attempt signed with (account.loginKeyGen).
//
// Returning true means "this attempt used a key that is now known to be wrong" — the
// caller should retry rather than retire the account. Returning false leaves the existing
// classification alone, so with no ModulusSource configured (every local-engine tool and
// test) the behaviour is exactly what it was.
func (m *Manager) staleLoginKey(err error, usedGen int) bool {
	var rejected *client.LoginRejectedError
	if !errors.As(err, &rejected) || rejected.Response != outOfDateResponse {
		return false
	}
	if m.cfg.ModulusSource == nil {
		return false
	}

	m.modMu.Lock()
	defer m.modMu.Unlock()
	// Somebody else already caught the rotation; this attempt predates it.
	if m.modGen > usedGen {
		return true
	}
	// Inside the recheck window, everyone gets the answer the account that actually did
	// the check got. Returning a flat false here instead would make a cohort rejected by
	// one event split arbitrarily between retried and retired, on nothing but which
	// account happened to reach this line first.
	if !m.modCheckedAt.IsZero() && time.Since(m.modCheckedAt) < modulusRecheckInterval {
		return m.modLastVerdict
	}
	m.modCheckedAt = time.Now()
	m.modLastVerdict = false

	fresh, ferr := m.cfg.ModulusSource()
	if ferr != nil || fresh == nil {
		// Keep the account retrying rather than retiring it. Not being able to read
		// the key is itself a sign the server is unwell, and "the key rotated AND the
		// client bundle is briefly unreachable" is exactly the shape of a restart —
		// precisely the moment an account must not be given up on. The cost of being
		// wrong is one attempt per account per reconnectBackoffMax while the error
		// text sits on the dashboard; the cost of being right is the whole roster.
		if ferr == nil {
			ferr = errors.New("the source returned no key")
		}
		m.logf("fleet: could not re-read the server's login key after a rejection: %v — keeping accounts in retry rather than retiring them", ferr)
		m.modLastVerdict = true
		return true
	}
	current := m.modulus
	if current == nil {
		current = m.cfg.Modulus
	}
	if current != nil && fresh.Cmp(current) == 0 {
		// The key is the same one we used, so response 6 meant something else —
		// a revision mismatch, or a cache table this process has stale. Drop the
		// cached /crc0 so the next attempt at least re-reads that, but do not
		// claim a rotation we did not observe.
		client.InvalidateChecksums()
		return false
	}
	m.modulus = fresh
	m.modGen++
	// The cache table is published by the same process that just restarted, so assume
	// it moved too rather than waiting out the checksum cache's own TTL.
	client.InvalidateChecksums()
	m.logf("fleet: the server's RSA login key rotated (generation %d) — reconnecting accounts will use the new one", m.modGen)
	m.modLastVerdict = true
	return true
}

// rotatedKeyRetry is staleLoginKey for one account: it asks with the generation that
// account's failed attempt actually used, and says so in that account's own activity log
// when the answer is yes, since "reconnect stopped: login failure requires attention" is
// otherwise the last thing the operator sees.
func (m *Manager) rotatedKeyRetry(a *account, err error) bool {
	a.mu.RLock()
	used := a.loginKeyGen
	a.mu.RUnlock()
	if !m.staleLoginKey(err, used) {
		return false
	}
	m.alogf(a, "[%s] rejected with a login key the server has since rotated — retrying with the new one", a.username)
	return true
}

// outOfDateResponse is the login server's "out of date" code. The engine sends it for a
// client revision mismatch, a cache CRC mismatch AND an RSA block it could not decrypt —
// see this file's own doc comment for why that ambiguity forces the check above to be
// evidence-based.
const outOfDateResponse = 6
