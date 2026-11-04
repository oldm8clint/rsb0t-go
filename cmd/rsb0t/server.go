package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"rsb0t-go/internal/serverprobe"
)

// The game server is the roster's wsURL and httpBase (plus, when the server's web client does not carry
// its login key where rsb0t can find it, a pasted modulus and exponent). rsb0t never names a server of its
// own: a fresh roster has none, and the setup page and `rsb0t server` ask for it.

// serverArgs is what a person gives for a server: its address, a pasted login key (both numbers empty
// when the key is to be found on the server), and, for a server whose game is not on its website's host
// and port, the game connection address.
type serverArgs struct {
	Address  string `json:"address"`
	Modulus  string `json:"modulus"`
	Exponent string `json:"exponent"`
	Game     string `json:"game"`
}

func (a serverArgs) options() serverprobe.Options {
	return serverprobe.Options{Modulus: a.Modulus, Exponent: a.Exponent, GameURL: a.Game}
}

// checkServer is serverprobe.Check, replaceable in tests.
var checkServer = func(addr string, opt serverprobe.Options) serverprobe.Report {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	return serverprobe.Check(ctx, addr, opt)
}

func (r *roster) str(k string) string {
	var s string
	_ = json.Unmarshal(r.fields[k], &s)
	return s
}

func (r *roster) set(k string, v any) {
	b, _ := json.Marshal(v)
	r.fields[k] = b
	r.ensureKey(k)
}

func (r *roster) del(k string) {
	delete(r.fields, k)
	for i, o := range r.order {
		if o == k {
			r.order = append(r.order[:i], r.order[i+1:]...)
			return
		}
	}
}

// serverInfo is the server part of the roster, as the setup page and `rsb0t server` show it.
type serverInfo struct {
	Address   string `json:"address"`
	WSURL     string `json:"wsURL"`
	HTTPBase  string `json:"httpBase"`
	KeyPasted bool   `json:"keyPasted"`
	Worlds    []int  `json:"worlds"`
}

func (r *roster) server() serverInfo {
	si := serverInfo{Address: r.str("server"), WSURL: r.str("wsURL"), HTTPBase: r.str("httpBase"), KeyPasted: r.str("modulus") != ""}
	_ = json.Unmarshal(r.fields["worldList"], &si.Worlds)
	if si.Address == "" {
		si.Address = si.HTTPBase
	}
	return si
}

func (r *roster) hasServer() bool { return r.str("wsURL") != "" && r.str("httpBase") != "" }

// applyServer writes what a check found into the roster: the endpoints, the worlds that answered, browser
// headers when the game connection wants them, and the login key only when it was pasted (a key found in
// the web client is read again at every start, so a server that changes its key keeps working). A new
// server also drops what was written by hand for the old one: its per-world addresses ("worlds").
func (r *roster) applyServer(rep serverprobe.Report) {
	if r.str("wsURL") != rep.Endpoints.WSURL || r.str("httpBase") != rep.Endpoints.HTTPBase {
		r.del("worlds")
	}
	r.set("server", rep.Address)
	r.set("wsURL", rep.Endpoints.WSURL)
	r.set("httpBase", rep.Endpoints.HTTPBase)
	if rep.KeyPasted {
		r.set("modulus", rep.Modulus)
		r.set("exponent", rep.Exponent)
	} else {
		r.del("modulus")
		r.del("exponent")
	}
	if len(rep.Worlds) > 1 {
		r.set("worldList", rep.Worlds)
	} else {
		r.set("worldList", []int{})
	}
	if rep.NeedsBrowserHeaders {
		r.set("browserHeaders", true)
	} else {
		r.del("browserHeaders")
	}
}

// printReport writes a check's steps and verdict for a terminal.
func printReport(w io.Writer, rep serverprobe.Report) {
	if rep.Endpoints.HTTPBase != "" {
		fmt.Fprintf(w, "Server: %s\n  website and game files: %s\n  game connection:        %s\n\n", rep.Address, rep.Endpoints.HTTPBase, rep.Endpoints.WSURL)
	}
	for _, s := range rep.Steps {
		mark := map[serverprobe.Status]string{serverprobe.OK: "OK     ", serverprobe.Warn: "Note   ", serverprobe.Fail: "Problem", serverprobe.Skip: "Skipped"}[s.Status]
		fmt.Fprintf(w, "  %s  %s: %s\n", mark, s.Name, s.Detail)
	}
	fmt.Fprintf(w, "\n%s\n", rep.Verdict)
}

const serverFlagHelp = `  ADDRESS          the server's website or one world: https://example-server.com, w1.example-server.com, localhost:8888
  -game ADDRESS    the game connection address, only when the server's staff say the game is on another
                   port or path (like wss://example-server.com:43595/)
  -modulus N       the server's login key, only when rsb0t cannot find it by itself (the long number)
  -exponent N      the second number of the login key (leave it out for 65537)
`

// serverFlags reads ADDRESS [-game ADDRESS] [-modulus N] [-exponent N] in any order. -h is flag.ErrHelp.
func serverFlags(name string, args []string) (serverArgs, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	m := fs.String("modulus", "", "")
	e := fs.String("exponent", "", "")
	g := fs.String("game", "", "")
	var rest []string
	for len(args) > 0 {
		if err := fs.Parse(args); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return serverArgs{}, err
			}
			return serverArgs{}, fmt.Errorf("%v (see `rsb0t %s -h`)", err, name)
		}
		args = fs.Args()
		if len(args) > 0 {
			rest, args = append(rest, args[0]), args[1:]
		}
	}
	if len(rest) > 1 {
		return serverArgs{}, fmt.Errorf("want one server address, got %d", len(rest))
	}
	a := serverArgs{Modulus: *m, Exponent: *e, Game: *g}
	if len(rest) == 1 {
		a.Address = rest[0]
	}
	return a, nil
}

// cmdCheckServer is `rsb0t check-server ADDRESS`: the read-only checks, nothing saved.
func cmdCheckServer(args []string) int {
	a, err := serverFlags("check-server", args)
	if errors.Is(err, flag.ErrHelp) {
		fmt.Print("rsb0t check-server ADDRESS [-game ADDRESS] [-modulus N -exponent N]\n\nChecks a game server, read-only, and says whether rsb0t can use it. Nothing is saved and nothing logs in.\n\n" + serverFlagHelp)
		return 0
	}
	if err == nil && a.Address == "" {
		err = errors.New("give the server's address, like `rsb0t check-server https://example-server.com`")
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "rsb0t check-server:", err)
		return 2
	}
	rep := checkServer(a.Address, a.options())
	printReport(os.Stdout, rep)
	if !rep.Ready {
		return 1
	}
	return 0
}

// cmdServer is `rsb0t server [ADDRESS]`: with an address it checks the server and saves it in the roster;
// without one it says which server the roster uses.
func cmdServer(home string, args []string) int {
	a, err := serverFlags("server", args)
	if errors.Is(err, flag.ErrHelp) {
		fmt.Print("rsb0t server ADDRESS [-game ADDRESS] [-modulus N -exponent N]\n\nChecks a game server and saves it as the one rsb0t plays on. With no address, says which server is saved.\n\n" + serverFlagHelp)
		return 0
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "rsb0t server:", err)
		return 2
	}
	ro, err := ensureRoster(home)
	if err != nil {
		fmt.Fprintln(os.Stderr, "rsb0t:", err)
		return 1
	}
	if a.Address == "" {
		if !ro.hasServer() {
			fmt.Println("rsb0t: no server is set yet. Set one with `rsb0t server <address>`, like `rsb0t server https://example-server.com`.")
			return 1
		}
		si := ro.server()
		fmt.Printf("server:   %s\nwebsite:  %s\ngame:     %s\n", si.Address, si.HTTPBase, si.WSURL)
		if si.KeyPasted {
			fmt.Println("login key: pasted (kept in the roster)")
		} else {
			fmt.Println("login key: read from the server's web client at every start")
		}
		return 0
	}
	code, msg := saveServer(ro, a, os.Stdout)
	fmt.Println(msg)
	return code
}

// saveServer checks addr and saves it when the address is usable. The exit code is 0 when the server is
// ready, 1 when it was saved but is not ready yet (or not saved at all), with a line saying which.
func saveServer(ro *roster, a serverArgs, w io.Writer) (int, string) {
	rep := checkServer(a.Address, a.options())
	printReport(w, rep)
	if !saveable(rep) {
		return 1, "rsb0t: nothing was saved; fix what is marked Problem and try again."
	}
	ro.applyServer(rep)
	if err := ro.save(); err != nil {
		return 1, "rsb0t: saving the roster: " + err.Error()
	}
	if !rep.Ready {
		return 1, "rsb0t: the address is saved in " + ro.path + ", but accounts cannot log in there until what is marked Problem above is fixed. " +
			"You can add accounts now; run this command again (with what it asks for) before you start the panel."
	}
	return 0, "rsb0t: saved in " + ro.path + ". Next: add an account with `rsb0t account add NAME`, or run `rsb0t` to open the setup page."
}

// saveable is whether a check found enough to save the server: the game files answered (so the address is
// right), and a pasted key, if any, was usable. A server whose key is still missing is saved, so the key can
// be pasted next.
func saveable(rep serverprobe.Report) bool {
	return rep.GameFilesOK && (!rep.KeyPastedBad || rep.KeyFound)
}

// setupServerFlag takes -server ADDRESS (and -game/-modulus/-exponent) off the front of the setup command's
// arguments, leaving the rest for the panel.
func setupServerFlag(args []string) (a serverArgs, rest []string) {
	for len(args) > 0 {
		if args[0] == "--" || !strings.HasPrefix(args[0], "-") {
			break
		}
		key, val, eq := strings.Cut(strings.TrimLeft(args[0], "-"), "=")
		if !eq {
			if len(args) < 2 {
				return a, args
			}
			val = args[1]
		}
		switch key {
		case "server":
			a.Address = val
		case "game":
			a.Game = val
		case "modulus":
			a.Modulus = val
		case "exponent":
			a.Exponent = val
		default:
			return a, args
		}
		if eq {
			args = args[1:]
		} else {
			args = args[2:]
		}
	}
	return a, args
}
