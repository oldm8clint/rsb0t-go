// Command rsb0t is the one program: it keeps a data folder, starts the panel and opens it.
//
//	rsb0t                       start the panel on http://127.0.0.1:7274/ (and open it)
//	rsb0t demo [N] [-- ...]     a simulated fleet of N accounts (default 12): nothing connects to a game
//	rsb0t setup [-server ADDR]  the first-run page: server, accounts, scripts and settings in the browser, then Start
//	rsb0t server ADDRESS        check a game server and save it in the roster (no address: show the current one)
//	rsb0t check-server ADDRESS  check a game server, read-only, and say whether rsb0t can use it
//	rsb0t init                  create the data folder and a starter roster
//	rsb0t account add NAME      add an account; the password is asked for, never shown, and kept in credentials.json
//	rsb0t account list          list the accounts and the script each starts with
//	rsb0t account script NAME SCRIPT   choose the script an account starts with (empty for none)
//	rsb0t account remove NAME   remove an account and its stored password
//	rsb0t scripts               list the scripts this build has
//	rsb0t version
//
// The server is any RuneScape 2 revision-289 server built on the LostCity engine: give the address of its
// website or of one world (https://example-server.com, w1.example-server.com, localhost:8888) and rsb0t
// works out the rest. A server key that cannot be found is pasted with -modulus and -exponent.
//
// Everything lives in one folder, rsb0t-data next to where the program is started (or -data DIR, or the
// RSB0T_HOME variable): fleet.config.json (the roster), credentials.json (passwords, this machine only),
// and what the panel saves (layout, statistics, travel heat map). Anything after `--` on the panel
// command is passed to the panel unchanged, so every panel flag still works:
//
//	rsb0t -- -listen 127.0.0.1:9000 -watchdog act
//
// The panel itself is internal/controlpanelapp, the program cmd/controlpanel also runs.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"

	"rsb0t-go/internal/bot"
	"rsb0t-go/internal/controlpanelapp"
)

// version is set at build time (-ldflags "-X main.version=v1.2.3"); a plain build reports its commit.
var version = ""

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	var data string
	noBrowser := false
	// Global flags come before the subcommand.
	for len(args) > 0 && strings.HasPrefix(args[0], "-") && args[0] != "--" {
		switch a := strings.TrimLeft(args[0], "-"); {
		case a == "data" && len(args) > 1:
			data, args = args[1], args[2:]
		case strings.HasPrefix(a, "data="):
			data, args = strings.TrimPrefix(a, "data="), args[1:]
		case a == "no-browser":
			noBrowser, args = true, args[1:]
		case a == "h" || a == "help":
			usage()
			return 0
		default:
			fmt.Fprintf(os.Stderr, "rsb0t: unknown option %s\n\n", args[0])
			usage()
			return 2
		}
	}
	home := dataDir(data)

	cmd, rest := "serve", args
	if len(args) > 0 && args[0] != "--" {
		cmd, rest = args[0], args[1:]
	} else if len(args) > 0 {
		rest = args[1:]
	}
	switch cmd {
	case "serve", "run", "start":
		return serve(home, rest, noBrowser)
	case "demo":
		n := 12
		if len(rest) > 0 && rest[0] != "--" {
			v, err := strconv.Atoi(rest[0])
			if err != nil || v < 1 {
				fmt.Fprintln(os.Stderr, "rsb0t demo: want a number of simulated accounts, like `rsb0t demo 12`")
				return 2
			}
			n, rest = v, rest[1:]
		}
		return demo(home, n, rest, noBrowser)
	case "setup":
		return setup(home, rest, noBrowser)
	case "init":
		ro, err := ensureRoster(home)
		if err != nil {
			fmt.Fprintln(os.Stderr, "rsb0t:", err)
			return 1
		}
		if !ro.hasServer() {
			fmt.Printf("rsb0t: data folder %s is ready; choose your server with `rsb0t server <address>`, then add an account with `rsb0t account add NAME`\n", home)
			return 0
		}
		fmt.Printf("rsb0t: data folder %s is ready; add an account with `rsb0t account add NAME`\n", home)
		return 0
	case "server":
		return cmdServer(home, rest)
	case "check-server", "check":
		return cmdCheckServer(rest)
	case "account", "accounts":
		return account(home, rest)
	case "scripts":
		names := bot.Names()
		sort.Strings(names)
		for _, n := range names {
			fmt.Println(n)
		}
		return 0
	case "version", "-v", "--version":
		fmt.Println("rsb0t", versionString())
		return 0
	case "help":
		usage()
		return 0
	}
	fmt.Fprintf(os.Stderr, "rsb0t: unknown command %q\n\n", cmd)
	usage()
	return 2
}

func usage() {
	fmt.Fprint(os.Stderr, `rsb0t-go (said "RS bot go"); the program is called rsb0t: a headless client and panel for a RuneScape 2 (revision 289) server

  rsb0t                      start the panel and open it in the browser
  rsb0t demo [N]             a simulated fleet of N accounts; connects to nothing
  rsb0t setup [-server ADDRESS]
                             the first-run page (server, accounts, scripts, settings), then start
  rsb0t server ADDRESS       check a game server and save it (ADDRESS: its website or one world,
                             like https://example-server.com or w1.example-server.com)
  rsb0t server ADDRESS -modulus N -exponent N
                             the same, with the server's login key pasted when it cannot be found
  rsb0t server ADDRESS -game GAMEADDRESS
                             the same, for a server whose game is on another address (wss://...)
  rsb0t server -h            what each of these means
  rsb0t check-server ADDRESS check a server without saving anything
  rsb0t init                 create the data folder and a starter roster
  rsb0t account add NAME     add an account (the password is asked for and kept on this machine)
  rsb0t account list         list accounts
  rsb0t account script NAME SCRIPT
  rsb0t account remove NAME
  rsb0t scripts              list the scripts in this build
  rsb0t version

options (before the command):
  -data DIR                  the data folder (default: $RSB0T_HOME, else ./rsb0t-data)
  -no-browser                do not open the browser

Anything after -- goes to the panel unchanged:  rsb0t -- -listen 127.0.0.1:9000

The server must be a RuneScape 2 revision-289 server (one built on the LostCity engine, for example);
other revisions will not work. Use rsb0t only on accounts you own, where the server's rules allow it.
`)
}

// dataDir is the data folder: -data, else RSB0T_HOME, else rsb0t-data in the working directory.
func dataDir(flagValue string) string {
	d := flagValue
	if d == "" {
		d = os.Getenv("RSB0T_HOME")
	}
	if d == "" {
		d = "rsb0t-data"
	}
	if abs, err := filepath.Abs(d); err == nil {
		return abs
	}
	return d
}

func versionString() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			if s.Key == "vcs.revision" && len(s.Value) >= 8 {
				return "dev-" + s.Value[:8]
			}
		}
	}
	return "dev"
}

// serve starts the panel on the roster in home. With no account yet it says how to add one.
func serve(home string, panelArgs []string, noBrowser bool) int {
	roster, err := ensureRoster(home)
	if err != nil {
		fmt.Fprintln(os.Stderr, "rsb0t:", err)
		return 1
	}
	if !roster.hasServer() {
		fmt.Printf("rsb0t: no game server chosen yet in %s, so this is a first run\n", filepath.Join(home, rosterFile))
		fmt.Println("  (or: `rsb0t server <address>` to choose it here, `rsb0t demo` to look around first)")
		return setup(home, panelArgs, noBrowser)
	}
	if len(roster.accounts()) == 0 {
		fmt.Printf("rsb0t: no accounts yet in %s, so this is a first run\n", filepath.Join(home, rosterFile))
		fmt.Println("  (or: `rsb0t account add NAME` to add one here, `rsb0t demo` to look around first)")
		return setup(home, panelArgs, noBrowser)
	}
	return launch(home, panelArgs, noBrowser, false)
}

// setup opens the first-run page and, once it says Start, runs the panel on what was set up.
func setup(home string, panelArgs []string, noBrowser bool) int {
	ro, err := ensureRoster(home)
	if err != nil {
		fmt.Fprintln(os.Stderr, "rsb0t:", err)
		return 1
	}
	for _, a := range panelArgs {
		if a == "--" {
			break
		}
		if a == "-h" || a == "-help" || a == "--help" {
			fmt.Print("rsb0t setup [-server ADDRESS [-game ADDRESS] [-modulus N -exponent N]] [-- panel options]\n\n" +
				"Opens the first-run page in the browser (server, accounts, scripts, settings); its Start button starts the panel.\n" +
				"With -server, the server is checked and saved first.\n\n" + serverFlagHelp)
			return 0
		}
	}
	sa, panelArgs := setupServerFlag(panelArgs)
	if sa.Address != "" {
		_, msg := saveServer(ro, sa, os.Stdout)
		fmt.Println(msg)
	}
	for len(panelArgs) > 0 && panelArgs[0] == "--" {
		panelArgs = panelArgs[1:]
	}
	if !runSetup(home, listenAddr(panelArgs), noBrowser) {
		return 0
	}
	return launch(home, panelArgs, noBrowser, false)
}

// demo starts the simulated fleet: no roster, no account, no network beyond the panel's own port.
func demo(home string, n int, panelArgs []string, noBrowser bool) int {
	if err := os.MkdirAll(home, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "rsb0t:", err)
		return 1
	}
	for len(panelArgs) > 0 && panelArgs[0] == "--" {
		panelArgs = panelArgs[1:]
	}
	return launch(home, append([]string{"-fake", strconv.Itoa(n)}, panelArgs...), noBrowser, true)
}

// launch runs the panel inside home, so the files it saves (statistics, heat map, script assignments)
// land in the data folder whatever the working directory was.
func launch(home string, extra []string, noBrowser, fake bool) int {
	if err := os.Chdir(home); err != nil {
		fmt.Fprintln(os.Stderr, "rsb0t:", err)
		return 1
	}
	args := []string{os.Args[0]}
	if !fake {
		args = append(args, "-config", rosterFile)
	}
	args = append(args, "-layout", "panel-layout.json", "-bank-state", "bank-state.json")
	for len(extra) > 0 && extra[0] == "--" {
		extra = extra[1:]
	}
	args = append(args, extra...)
	os.Args = args

	url := "http://" + listenAddr(extra) + "/"
	fmt.Printf("rsb0t %s: the panel is at %s (Ctrl+C stops it and logs every account out)\n", versionString(), url)
	if !noBrowser {
		go openWhenUp(url)
	}
	controlpanelapp.Run()
	return 0
}

// listenAddr is the panel's address as the extra arguments set it, else the default.
func listenAddr(extra []string) string {
	for i, a := range extra {
		a = strings.TrimLeft(a, "-")
		if a == "listen" && i+1 < len(extra) {
			return extra[i+1]
		}
		if strings.HasPrefix(a, "listen=") {
			return strings.TrimPrefix(a, "listen=")
		}
	}
	return defaultListen
}
