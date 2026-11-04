// Command worldprobe answers two questions about a multi-world deployment, and refuses to
// pretend it answered either one when it did not.
//
//	worldprobe -ws wss://w1.example.invalid/ -http https://w1.example.invalid/ -worlds 1,2,3
//
// DEFAULT MODE IS READ-ONLY AND SAFE AGAINST PRODUCTION. It makes one ordinary GET of each
// world's /crc0 — the same route every login already fetches, dozens of times a minute on a
// live fleet — and reports whether the world is there, whether its checksums match world
// one's, and whether its RSA login modulus matches. No login, no game WebSocket, no
// credentials, no cheats. It cannot take a connection slot or spend a login-rate budget.
//
//	worldprobe ... -login -user throwaway1 -password-env RSB0T_PROBE_PASSWORD -world 2
//
// LOGIN MODE is the capacity experiment in docs/multiworld.md, and it is deliberately
// awkward to invoke: it performs exactly ONE login attempt, on ONE named world, with ONE
// named account, and prints the login RESPONSE CODE by name rather than "failed". That is
// the whole point — "response 7, This world is full" and "response 9, the address is at its
// connection ceiling" are completely different answers that a generic error string makes
// indistinguishable, and telling them apart is the experiment.
//
// WHAT IT WILL NOT DO:
//
//   - It never enables cheats. There is no -enable-cheats flag, and there is nothing here
//     that would use one. Per CLAUDE.md, a cheat-enabled tool must never be pointed at a
//     production world; the simplest way to honour that is to have no such mode at all.
//   - It never reads the fleet's roster, so it cannot pick up a real account by accident.
//     -user is explicit or there is no login.
//   - It never prints a password, and takes one only from an environment variable.
//   - It logs out immediately on success. It is a probe, not a session.
//
// RUN LOGIN MODE WITH A THROWAWAY ACCOUNT, not one the live fleet is using: a successful
// probe login occupies that account, and if the fleet then tries to log the same account in
// it gets response 5.
package main

import (
	"flag"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"rsb0t-go/internal/client"
	"rsb0t-go/internal/fleet"
	"rsb0t-go/internal/worlds"
)

// responseName turns a login response code into what the game's own client says about it,
// so a run's output is readable without a lookup table. The strings are the vendored
// client's (src/client/shell/Client.ts's loginMes1/loginMes2), which CLAUDE.md names as the
// protocol authority.
func responseName(code int) string {
	switch code {
	case 2, 15:
		return "SUCCESS"
	case 3:
		return "invalid username or password"
	case 4:
		return "account disabled"
	case 5:
		return "ALREADY LOGGED IN (this account has a session somewhere)"
	case 6:
		return "client out of date / RSA key mismatch"
	case 7:
		return "THIS WORLD IS FULL — please use a different world"
	case 8:
		return "login server offline"
	case 9:
		return "LOGIN LIMIT EXCEEDED — too many connections from this address"
	case 11:
		return "login server rejected the session"
	case 12:
		return "members-only world, free account"
	case 16:
		return "LOGIN ATTEMPTS EXCEEDED — the address's rate budget is spent"
	case 21:
		return "WORLD-HOP TIMER — logged out of another world too recently"
	default:
		return "unrecognised"
	}
}

func main() {
	ws := flag.String("ws", "wss://w1.example.invalid/", "the fleet's own WebSocket URL; other worlds are derived by rewriting its leading wN. label")
	httpBase := flag.String("http", "https://w1.example.invalid/", "the fleet's own HTTP base")
	list := flag.String("worlds", "1,2,3", "comma-separated world numbers to check")
	doLogin := flag.Bool("login", false, "ALSO attempt ONE real login on -world with -user. This is the capacity experiment in docs/multiworld.md; read it first. Use a throwaway account, never one the live fleet is running")
	world := flag.Int("world", 0, "with -login: which world to attempt the login on")
	user := flag.String("user", "", "with -login: the account name. Must be a throwaway")
	passwordEnv := flag.String("password-env", "RSB0T_PROBE_PASSWORD", "with -login: the environment variable holding the password. The password is never taken from a flag and never printed")
	flag.Parse()

	nums, err := parseWorlds(*list)
	if err != nil {
		fmt.Fprintln(os.Stderr, "worldprobe:", err)
		os.Exit(2)
	}
	wm, err := worlds.New(*ws, *httpBase, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "worldprobe:", err)
		os.Exit(2)
	}
	if err := wm.List(nums); err != nil {
		fmt.Fprintln(os.Stderr, "worldprobe:", err)
		os.Exit(2)
	}

	fmt.Printf("worldprobe: checking worlds %v (read-only: one GET of each /crc0 and /client/client.js, no login)\n\n", nums)
	probes := fleet.ProbeWorlds(wm)
	for _, line := range fleet.DescribeWorlds(probes) {
		fmt.Println(" ", line)
	}
	fmt.Println()

	// The two things that decide whether "a world is just a different wsURL" holds: are the
	// nine archive checksums the login block carries the same, and is the RSA key the same?
	// Measured rather than assumed — if either differs, per-world handling is needed and
	// this prints exactly which.
	compare(wm, nums)

	if !*doLogin {
		fmt.Println("\nworldprobe: read-only checks done. Pass -login to run the capacity experiment (docs/multiworld.md).")
		return
	}

	if *world <= 0 || *user == "" {
		fmt.Fprintln(os.Stderr, "\nworldprobe: -login needs both -world and -user. Refusing to guess either.")
		os.Exit(2)
	}
	password := os.Getenv(*passwordEnv)
	if password == "" {
		fmt.Fprintf(os.Stderr, "\nworldprobe: -login needs a password in $%s. It is never accepted as a flag.\n", *passwordEnv)
		os.Exit(2)
	}
	e, err := wm.Endpoints(*world)
	if err != nil {
		fmt.Fprintln(os.Stderr, "worldprobe:", err)
		os.Exit(2)
	}

	mod, err := fleet.FetchProductionModulus(e.HTTPBase)
	if err != nil {
		fmt.Fprintf(os.Stderr, "worldprobe: could not read world %d's login key: %v\n", *world, err)
		os.Exit(2)
	}

	fmt.Printf("\nworldprobe: ONE login attempt as %q on world %d (%s)\n", *user, *world, e.WSURL)
	started := time.Now()
	sess, err := client.Connect(client.Config{
		WSURL:    e.WSURL,
		HTTPBase: e.HTTPBase,
		Username: *user,
		Password: password,
		Modulus:  mod,
		Exponent: big.NewInt(65537),
	})
	took := time.Since(started).Round(time.Millisecond)

	if err == nil {
		fmt.Printf("  response 2  SUCCESS — logged in to world %d in %s\n", *world, took)
		fmt.Println("  logging straight back out (this is a probe, not a session)")
		_ = sess.Logout()
		fmt.Println("\nworldprobe: this world admitted this account from this address.")
		return
	}

	var rejected *client.LoginRejectedError
	if ok := asRejected(err, &rejected); ok {
		extra := ""
		if rejected.Response == client.HopTimerResponse && rejected.RetrySeconds > 0 {
			extra = fmt.Sprintf(" (%d seconds to wait)", rejected.RetrySeconds)
		}
		fmt.Printf("  response %-2d %s%s   [%s]\n", rejected.Response, responseName(rejected.Response), extra, took)
		fmt.Println()
		switch rejected.Response {
		case client.WorldFullResponse:
			fmt.Printf("worldprobe: world %d is FULL. This is the population cap (1,000 by default), not a\n", *world)
			fmt.Println("            limit on this address. Try another world; do not read this as a rate limit.")
		case 9:
			fmt.Printf("worldprobe: this ADDRESS is at its concurrent-connection ceiling for world %d.\n", *world)
			fmt.Println("            If a login on a DIFFERENT world from this same address now succeeds, the")
			fmt.Println("            ceiling is per address PER WORLD and spreading multiplies capacity.")
			fmt.Println("            If it is refused the same way, the ceiling is per address OVERALL.")
		case 16:
			fmt.Println("worldprobe: this address's login RATE budget is spent. Whether a login on another")
			fmt.Println("            world is admitted right now is the second half of the experiment.")
		case client.HopTimerResponse:
			fmt.Println("worldprobe: the server's own world-hop timer. Use a second throwaway account for the")
			fmt.Println("            other world rather than waiting this out.")
		}
		return
	}
	fmt.Printf("  no login response at all after %s: %v\n", took, err)
	fmt.Println("\nworldprobe: the attempt did not reach the login exchange, so it measured nothing.")
	os.Exit(2)
}

// asRejected is errors.As with a concrete type, kept as a helper so main reads as prose.
func asRejected(err error, target **client.LoginRejectedError) bool {
	for err != nil {
		if r, ok := err.(*client.LoginRejectedError); ok {
			*target = r
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

// compare fetches each world's /crc0 and login modulus and reports whether they match the
// first world's. This is the check that decides whether per-world CRC tables and per-world
// key handling are needed; it was answered "identical" for w1/w2/w3 on 2026-09-22 and is
// here so the next world can be answered the same way rather than assumed.
func compare(wm *worlds.Map, nums []int) {
	type fingerprint struct {
		crc string
		mod string
		err error
	}
	fps := map[int]fingerprint{}
	for _, n := range nums {
		e, err := wm.Endpoints(n)
		if err != nil {
			fps[n] = fingerprint{err: err}
			continue
		}
		var fp fingerprint
		fp.crc, fp.err = fetchCRC(e.HTTPBase)
		if fp.err == nil {
			if mod, err := fleet.FetchProductionModulus(e.HTTPBase); err == nil {
				fp.mod = mod.String()
			} else {
				fp.err = err
			}
		}
		fps[n] = fp
	}

	base := nums[0]
	ref := fps[base]
	if ref.err != nil {
		fmt.Printf("  world %d could not be fingerprinted (%v) — nothing to compare against\n", base, ref.err)
		return
	}
	for _, n := range nums {
		fp := fps[n]
		if fp.err != nil {
			fmt.Printf("  world %d: not comparable (%v)\n", n, fp.err)
			continue
		}
		crcSame, modSame := fp.crc == ref.crc, fp.mod == ref.mod
		switch {
		case crcSame && modSame:
			fmt.Printf("  world %d: /crc0 IDENTICAL to world %d, RSA modulus IDENTICAL — a world is just a different wsURL\n", n, base)
		case !crcSame && !modSame:
			fmt.Printf("  world %d: /crc0 DIFFERS and RSA modulus DIFFERS from world %d — this world needs its own crcTable AND its own key\n", n, base)
		case !crcSame:
			fmt.Printf("  world %d: /crc0 DIFFERS from world %d — this world needs its own crcTable\n", n, base)
		default:
			fmt.Printf("  world %d: RSA modulus DIFFERS from world %d — this world needs its own key handling\n", n, base)
		}
	}
}

// fetchCRC returns the first 36 bytes of a world's /crc0 as hex — the nine archive
// checksums the login block carries, and nothing else.
func fetchCRC(httpBase string) (string, error) {
	c := &http.Client{Timeout: 10 * time.Second}
	resp, err := c.Get(strings.TrimRight(httpBase, "/") + "/crc0")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var buf [36]byte
	if _, err := io.ReadFull(resp.Body, buf[:]); err != nil {
		return "", fmt.Errorf("short /crc0: %w", err)
	}
	return fmt.Sprintf("%x", buf), nil
}

func parseWorlds(s string) ([]int, error) {
	var out []int
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		n, err := strconv.Atoi(part)
		if err != nil {
			return nil, fmt.Errorf("%q is not a world number", part)
		}
		out = append(out, n)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no worlds given")
	}
	return out, nil
}
