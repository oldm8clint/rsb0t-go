package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strings"
	"time"

	"rsb0t-go/internal/bot"
	"rsb0t-go/internal/serverprobe"
	"rsb0t-go/internal/settings"
)

//go:embed setup.html
var setupPage []byte

// setupServer is the first-run page: add accounts, pick each one's script and settings, then start the
// panel. It binds the loopback address only, answers only requests whose Host is that address, and wants a
// custom header on every change, so a web page in the same browser cannot drive it.
type setupServer struct {
	home  string
	start chan struct{}
}

const setupHeader = "X-Rsb0t-Setup"

// runSetup serves the setup page on addr until the page's Start button is pressed (true) or Ctrl+C (false).
func runSetup(home, addr string, noBrowser bool) bool {
	if !isLoopback(addr) {
		fmt.Fprintf(os.Stderr, "rsb0t: setup is served on loopback addresses only, not %s\n", addr)
		return false
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "rsb0t: setup cannot listen on %s: %v\n", addr, err)
		return false
	}
	s := &setupServer{home: home, start: make(chan struct{}, 1)}
	srv := &http.Server{Handler: s.handler(), ReadHeaderTimeout: 10 * time.Second}
	go srv.Serve(ln)
	url := "http://" + addr + "/"
	fmt.Printf("rsb0t: the start page is at %s (your server, accounts and settings are there; press Start)\n", url)
	if !noBrowser {
		go openWhenUp(url)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	started := false
	select {
	case <-s.start:
		started = true
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdown)
	return started
}

func isLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (s *setupServer) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(setupPage)
	})
	mux.HandleFunc("/api/setup/state", s.state)
	mux.HandleFunc("/api/setup/accounts", s.accounts)
	mux.HandleFunc("/api/setup/settings", s.accountSettings)
	mux.HandleFunc("/api/setup/start", s.startPanel)
	mux.HandleFunc("/api/setup/check", s.checkServer)
	mux.HandleFunc("/api/setup/server", s.saveServer)
	return s.guard(mux)
}

// guard refuses a request whose Host is not a loopback name, and a change that lacks the header or is not
// JSON (a cross-site form post can send neither).
func (s *setupServer) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		host = strings.Trim(host, "[]")
		if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
			http.Error(w, "this page answers on the loopback address only", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if r.Header.Get(setupHeader) != "1" {
				http.Error(w, "missing "+setupHeader, http.StatusForbidden)
				return
			}
			if r.Method != http.MethodDelete && !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
				http.Error(w, "want application/json", http.StatusUnsupportedMediaType)
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, code int, format string, a ...any) {
	writeJSON(w, code, map[string]string{"error": fmt.Sprintf(format, a...)})
}

// field is one setting as the page draws it.
type field struct {
	Key     string   `json:"key"`
	Type    string   `json:"type"`
	Label   string   `json:"label"`
	Help    string   `json:"help,omitempty"`
	Group   string   `json:"group,omitempty"`
	Default any      `json:"default"`
	Min     *float64 `json:"min,omitempty"`
	Max     *float64 `json:"max,omitempty"`
	Options []string `json:"options,omitempty"`
}

type scriptInfo struct {
	Name   string  `json:"name"`
	Fields []field `json:"fields"` // empty: configure it as a JSON object
	// NeedsMap is true for a script that walks and refuses to start without the map data (the roster's
	// packPath, the collision pack).
	NeedsMap bool `json:"needsMap"`
}

// needsMapData are the scripts that refuse to start without a collision pack (internal/bot: each checks
// ctx.Pack == nil at start). TestNeedsMapDataNamesRealScripts keeps the names honest.
var needsMapData = map[string]bool{
	"AntipoisonCollector": true, "AntipoisonMaker": true, "BankFletcher": true, "BarbarianAgility": true,
	"BowStringer": true, "MasterCrafter": true, "CombatTrainer": true, "EssenceMiner": true, "FishingTrawler": true,
	"FlaxPicker": true, "GemCutter": true, "HerbCleaner": true, "LootChestFisher": true, "MuleReceiver": true,
	"ProgressiveCrafter": true, "ProgressiveFisher": true, "ProgressiveThiever": true, "ProgressiveWCFletch": true,
	"ProgressiveWoodcutter": true, "QuestLadder": true, "RuneCarrier": true, "RuneRunner": true, "ShopBuyout": true,
	"SmelterBot": true, "ToadCollector": true, "VialFiller": true,
}

type accountInfo struct {
	Username    string         `json:"username"`
	Script      string         `json:"script"`
	HasPassword bool           `json:"hasPassword"`
	Settings    map[string]any `json:"settings"`
}

// fieldsOf turns a script's typed schema into form fields, grouped and in key order.
func fieldsOf(sc settings.Schema) []field {
	out := make([]field, 0, len(sc))
	for k, d := range sc {
		f := field{Key: k, Type: string(d.Type), Label: d.Label, Help: d.Help, Group: d.Group, Min: d.Min, Max: d.Max, Options: d.Options}
		switch v := d.Default.(type) {
		case bool, float64, string, []string:
			f.Default = v
		case nil:
		default:
			f.Default = fmt.Sprint(v)
		}
		if f.Label == "" {
			f.Label = k
		}
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Group != out[j].Group {
			return out[i].Group < out[j].Group
		}
		return out[i].Key < out[j].Key
	})
	return out
}

func (s *setupServer) scripts() []scriptInfo {
	names := bot.Names()
	sort.Strings(names)
	out := make([]scriptInfo, 0, len(names))
	for _, n := range names {
		si := scriptInfo{Name: n, Fields: []field{}, NeedsMap: needsMapData[n]}
		if sc, ok := bot.SettingsSchema(n); ok {
			si.Fields = fieldsOf(sc)
		}
		out = append(out, si)
	}
	return out
}

func (s *setupServer) snapshot() (map[string]any, error) {
	r, err := ensureRoster(s.home)
	if err != nil {
		return nil, err
	}
	creds, err := loadCredentials(s.home)
	if err != nil {
		return nil, err
	}
	accts := []accountInfo{}
	for _, a := range r.accounts() {
		var st map[string]any
		_ = json.Unmarshal(a["settings"], &st)
		if st == nil {
			st = map[string]any{}
		}
		accts = append(accts, accountInfo{Username: a.name(), Script: a.script(), HasPassword: creds.passwordFor(a.name()) != "", Settings: st})
	}
	o, err := loadOptions(s.home)
	if err != nil {
		return nil, err
	}
	out := map[string]any{"accounts": accts, "scripts": s.scripts(), "home": s.home, "hasMapData": r.str("packPath") != "", "skipSetup": o.SkipSetup}
	if r.hasServer() {
		out["server"] = r.server()
	}
	return out, nil
}

func (s *setupServer) state(w http.ResponseWriter, r *http.Request) {
	snap, err := s.snapshot()
	if err != nil {
		fail(w, 500, "%v", err)
		return
	}
	writeJSON(w, 200, snap)
}

// accounts: POST {username, password, script} adds one; DELETE ?name= removes one and its password.
func (s *setupServer) accounts(w http.ResponseWriter, r *http.Request) {
	ro, err := ensureRoster(s.home)
	if err != nil {
		fail(w, 500, "%v", err)
		return
	}
	switch r.Method {
	case http.MethodPost:
		var in struct{ Username, Password, Script string }
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			fail(w, 400, "that was not a valid request: %v", err)
			return
		}
		in.Username = strings.TrimSpace(in.Username)
		if why := checkName(in.Username); why != "" {
			fail(w, 400, "%q %s", in.Username, why)
			return
		}
		if in.Password == "" {
			fail(w, 400, "the password is empty")
			return
		}
		if ro.find(in.Username) >= 0 {
			fail(w, 409, "%s is already in the roster", in.Username)
			return
		}
		if in.Script != "" && !scriptKnown(in.Script) {
			fail(w, 400, "there is no script called %q", in.Script)
			return
		}
		creds, err := loadCredentials(s.home)
		if err == nil {
			creds[in.Username] = in.Password
			err = creds.save(s.home)
		}
		if err != nil {
			fail(w, 500, "saving the password: %v", err)
			return
		}
		entry := accountEntry{}
		entry["username"], _ = json.Marshal(in.Username)
		entry["script"], _ = json.Marshal(in.Script)
		entry["world"] = json.RawMessage("0")
		entry["settings"] = json.RawMessage("{}")
		ro.setAccounts(append(ro.accounts(), entry))
		if err := ro.save(); err != nil {
			fail(w, 500, "saving the roster: %v", err)
			return
		}
	case http.MethodDelete:
		name := r.URL.Query().Get("name")
		i := ro.find(name)
		if i < 0 {
			fail(w, 404, "there is no account %q", name)
			return
		}
		list := ro.accounts()
		ro.setAccounts(append(list[:i], list[i+1:]...))
		if err := ro.save(); err != nil {
			fail(w, 500, "saving the roster: %v", err)
			return
		}
		creds, err := loadCredentials(s.home)
		if err == nil {
			creds.delete(name)
			err = creds.save(s.home)
		}
		if err != nil {
			fail(w, 500, "the account was removed but its stored password was not: %v", err)
			return
		}
	default:
		fail(w, 405, "POST adds an account, DELETE removes one")
		return
	}
	s.state(w, r)
}

// accountSettings: PUT {username, script, settings} sets an account's script and settings. Settings are
// checked against the script's typed schema when it has one: an unknown key, or a value of the wrong type, is
// refused with a message naming it.
func (s *setupServer) accountSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		fail(w, 405, "PUT sets an account's script and settings")
		return
	}
	var in struct {
		Username string
		Script   string
		Settings map[string]any
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		fail(w, 400, "that was not a valid request: %v", err)
		return
	}
	ro, err := ensureRoster(s.home)
	if err != nil {
		fail(w, 500, "%v", err)
		return
	}
	i := ro.find(in.Username)
	if i < 0 {
		fail(w, 404, "there is no account %q", in.Username)
		return
	}
	if in.Script != "" && !scriptKnown(in.Script) {
		fail(w, 400, "there is no script called %q", in.Script)
		return
	}
	if in.Settings == nil {
		in.Settings = map[string]any{}
	}
	if err := checkSettings(in.Script, in.Settings); err != nil {
		fail(w, 400, "%v", err)
		return
	}
	list := ro.accounts()
	list[i]["script"], _ = json.Marshal(in.Script)
	list[i]["settings"], _ = json.Marshal(in.Settings)
	ro.setAccounts(list)
	if err := ro.save(); err != nil {
		fail(w, 500, "saving the roster: %v", err)
		return
	}
	s.state(w, r)
}

// checkSettings is why settings cannot be saved for script, nil when they can.
func checkSettings(script string, st map[string]any) error {
	sc, typed := bot.SettingsSchema(script)
	if !typed {
		return nil
	}
	for k, v := range st {
		def, ok := sc[k]
		if !ok {
			return fmt.Errorf("%s has no setting called %q", script, k)
		}
		switch def.Type {
		case settings.TypeBoolean:
			if _, ok := v.(bool); !ok {
				return fmt.Errorf("%s: %q wants true or false", script, k)
			}
		case settings.TypeNumber:
			n, ok := v.(float64)
			if !ok {
				return fmt.Errorf("%s: %q wants a number", script, k)
			}
			if def.Min != nil && n < *def.Min || def.Max != nil && n > *def.Max {
				return fmt.Errorf("%s: %q is out of range", script, k)
			}
		case settings.TypeString, settings.TypeTile:
			if _, ok := v.(string); !ok {
				return fmt.Errorf("%s: %q wants text", script, k)
			}
		case settings.TypeList:
			if _, ok := v.([]any); !ok {
				return fmt.Errorf("%s: %q wants a list", script, k)
			}
		}
	}
	return nil
}

func (s *setupServer) startPanel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		fail(w, 405, "POST starts the panel")
		return
	}
	ro, err := ensureRoster(s.home)
	if err != nil {
		fail(w, 500, "%v", err)
		return
	}
	if !ro.hasServer() {
		fail(w, 400, "choose your game server first (the Server box at the top), or close this page and run `rsb0t demo`")
		return
	}
	if len(ro.accounts()) == 0 {
		fail(w, 400, "add at least one account first, or close this page and run `rsb0t demo`")
		return
	}
	creds, err := loadCredentials(s.home)
	if err != nil {
		fail(w, 500, "%v", err)
		return
	}
	for _, a := range ro.accounts() {
		if creds.passwordFor(a.name()) == "" {
			fail(w, 400, "%s has no password yet: remove it under Your accounts and add it again with its password", a.name())
			return
		}
	}
	// The panel stops at once when the server cannot be reached, and a double-click's window closes with it: say so here.
	if rep := checkServer(ro.server().Address, serverprobe.Options{GameURL: ro.str("wsURL")}); !rep.GameFilesOK {
		fail(w, 502, "the server is not answering, so nothing was started: %s", rep.Verdict)
		return
	}
	var body struct {
		SkipSetup bool `json:"skipSetup"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	o, err := loadOptions(s.home)
	if err != nil {
		fail(w, 500, "%v", err)
		return
	}
	o.SkipSetup = body.SkipSetup
	if err := saveOptions(s.home, o); err != nil {
		fail(w, 500, "%v", err)
		return
	}
	writeJSON(w, 200, map[string]bool{"starting": true})
	select {
	case s.start <- struct{}{}:
	default:
	}
}

// readServerRequest reads what the Server box sends: the address as typed, a key when one was pasted, and
// a game connection address when one was given.
func readServerRequest(w http.ResponseWriter, r *http.Request) (serverArgs, bool) {
	var in serverArgs
	if r.Method != http.MethodPost {
		fail(w, 405, "POST checks a server")
		return in, false
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		fail(w, 400, "that was not a valid request: %v", err)
		return in, false
	}
	if strings.TrimSpace(in.Address) == "" {
		fail(w, 400, "type the address of your server first, like https://example-server.com")
		return in, false
	}
	return in, true
}

// checkServer: POST {address, modulus, exponent} runs the read-only checks and answers the report;
// nothing is saved.
func (s *setupServer) checkServer(w http.ResponseWriter, r *http.Request) {
	in, ok := readServerRequest(w, r)
	if !ok {
		return
	}
	writeJSON(w, 200, map[string]any{"report": checkServer(in.Address, in.options())})
}

// saveServer: POST {address, modulus, exponent} runs the checks and, when the address is a game server,
// saves it in the roster. The answer is the report, whether it was saved, and the new state.
func (s *setupServer) saveServer(w http.ResponseWriter, r *http.Request) {
	in, ok := readServerRequest(w, r)
	if !ok {
		return
	}
	ro, err := ensureRoster(s.home)
	if err != nil {
		fail(w, 500, "%v", err)
		return
	}
	rep := checkServer(in.Address, in.options())
	saved := saveable(rep)
	if saved {
		ro.applyServer(rep)
		if err := ro.save(); err != nil {
			fail(w, 500, "saving the roster: %v", err)
			return
		}
	}
	snap, err := s.snapshot()
	if err != nil {
		fail(w, 500, "%v", err)
		return
	}
	snap["report"], snap["saved"] = rep, saved
	writeJSON(w, 200, snap)
}
