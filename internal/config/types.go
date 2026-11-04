package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"
)

// Version is the settings file format this package reads. Additive keys do not bump it; a change
// an old file would silently misread does.
const Version = 1

// Config is a loaded settings file: validated, with every default filled in, relative paths made
// absolute and passwords resolved for enabled accounts.
type Config struct {
	// Path is the file Load read, empty after Parse.
	Path string
	// Legacy is true when the file was in the fleet.config.json format cmd/fleet reads.
	Legacy bool
	// Warnings are problems that did not stop loading, such as a plaintext password or an
	// ignored key in a legacy file. They never quote a password.
	Warnings []string
	Global   Global
	Accounts []Account
}

// Enabled returns the accounts that should run, in file order.
func (c *Config) Enabled() []*Account {
	var out []*Account
	for i := range c.Accounts {
		if c.Accounts[i].Enabled {
			out = append(out, &c.Accounts[i])
		}
	}
	return out
}

// Global holds the process-wide knobs. Accounts cannot override these.
type Global struct {
	Server       Server       `json:"server"`
	Data         Data         `json:"data"`
	Login        Login        `json:"login"`
	Runtime      Runtime      `json:"runtime"`
	Log          Log          `json:"log"`
	Telemetry    Telemetry    `json:"telemetry"`
	ControlPanel ControlPanel `json:"controlPanel"`
}

// Server says which game server every account dials. Profile picks one entry of Profiles; the
// built-in "live" (the live server) and "local" (a local Engine-TS) entries apply unless the file defines a
// profile of the same name, which then replaces the built-in one whole.
type Server struct {
	Profile  string                   `json:"profile"`
	Profiles map[string]ServerProfile `json:"profiles"`
	// World and Members are what the TS client is constructed with (its nodeid is World+9).
	// Why: the server picks the world by the host dialled, so these only shape client-side state.
	World   int  `json:"world"`
	Members bool `json:"members"`
	// LowMemory is the login block's low-memory flag, client.Config.LowMem.
	LowMemory bool `json:"lowMemory"`
	// AntiCheatPackets sends the ANTICHEAT_OPLOGIC side-packets a browser client sends
	// (client.Config.NoAntiCheat is its inverse). On by default; turn it off only for accounts
	// whose owner has no detection concern.
	AntiCheatPackets bool     `json:"antiCheatPackets"`
	DialTimeout      Duration `json:"dialTimeout"`
	// ReadTimeout matches ClientStream's 30 s read timeout: a socket silent that long is dead.
	ReadTimeout Duration `json:"readTimeout"`
	// Active is the selected profile, normalised. Connection code reads this, not Profiles.
	Active ServerProfile `json:"-"`
}

// ServerProfile is one server: its WebSocket, the HTTP base the login data is fetched from, and
// overrides for that login data.
//
// client.Connect fetches the nine archive checksums from HTTPBase+"crc0", and cmd/fleet scrapes
// the RSA modulus from HTTPBase+"client/client.js". CRCTable, RSAModulus and RSAModulusFile replace
// those fetches; HTTPBase may be empty only when both are set. Only "live" needs no key of its
// own: a local engine generates its own key pair, usually with an exponent other than 65537, so
// any other active profile must set a modulus and RSAExponent. Files are read only for the active
// profile.
type ServerProfile struct {
	URL string `json:"url"`
	// HTTPBase always ends in "/" after loading, because callers append paths to it.
	HTTPBase string `json:"httpBase"`
	// CRCTable holds the nine archive checksums sent in the login block. Signed values are
	// accepted because the TS client holds them in an Int32Array.
	CRCTable []int64 `json:"crcTable"`
	// CRC is CRCTable as unsigned values, nil when the checksums are fetched.
	CRC []uint32 `json:"-"`
	// RSAModulus is the login modulus in decimal. The live server rotates its key pair on restart, so
	// RSAModulusFile lets an operator swap the key without editing this file; after loading the
	// active profile's RSAModulus holds the file's contents.
	RSAModulus     string `json:"rsaModulus"`
	RSAModulusFile string `json:"rsaModulusFile"`
	RSAExponent    string `json:"rsaExponent"`
}

// JagChecksums returns CRC as client.Config.Checksums wants it, nil when the checksums are fetched.
func (p ServerProfile) JagChecksums() *[9]int32 {
	if len(p.CRC) != 9 {
		return nil
	}
	var out [9]int32
	for i, v := range p.CRC {
		out[i] = int32(v)
	}
	return &out
}

// Data says where read-only data shared by every account comes from.
type Data struct {
	// Collision is the rsb0t collision pack (collision.lcnav or .lcnav.gz) internal/pathfind loads.
	Collision string `json:"collision"`
}

// Login holds the process-wide login knobs.
type Login struct {
	// MaxSessions caps the accounts logged in at once; 0 means no cap.
	MaxSessions int `json:"maxSessions"`
	// LoginDelay is a pause between successive logins; 0s means none.
	LoginDelay Duration `json:"loginDelay"`
	// CredentialsFile holds passwords outside the repository, as username:password lines (the
	// format rsb0t's e2e/headless-parity-live.ts reads) or a JSON object of username to password.
	CredentialsFile string `json:"credentialsFile"`
}

// Runtime holds the Go runtime knobs Apply sets at startup.
type Runtime struct {
	// MemoryLimit is the process's soft memory limit: "off" (the default) or a size such as
	// "512MiB". Why off: a tight limit makes the collector run continuously once live data nears
	// it, which costs far more CPU than the memory it saves.
	MemoryLimit string `json:"memoryLimit"`
	// GCPercent is GOGC; -1 turns the collector off and is refused when the limit is off.
	GCPercent int `json:"gcPercent"`
	// MaxProcs is GOMAXPROCS; 0 leaves the runtime's choice.
	MaxProcs int `json:"maxProcs"`
	// MemoryLimitBytes is MemoryLimit resolved, math.MaxInt64 when off.
	MemoryLimitBytes int64 `json:"-"`
}

// Log configures logging. Output is "shared" (one stream, File or stderr) or "per-account" (one
// file per account under Dir).
type Log struct {
	Level  string `json:"level"`
	Output string `json:"output"`
	File   string `json:"file"`
	Dir    string `json:"dir"`
}

// Telemetry is off by default because a report costs a goroutine and a stop-the-world pause.
type Telemetry struct {
	// MemReportEvery logs runtime.ReadMemStats at this interval; 0s is off.
	MemReportEvery Duration `json:"memReportEvery"`
}

// ControlPanel is the local panel-based control page served to the user's own browser
// (cmd/controlpanel). It is off by default.
type ControlPanel struct {
	Enabled bool `json:"enabled"`
	// Listen is host:port. A non-loopback host is refused unless an access token is configured.
	Listen string `json:"listen"`
	// The optional access token comes from the environment variable TokenEnv or the file
	// TokenFile, never from this file.
	TokenEnv  string `json:"tokenEnv"`
	TokenFile string `json:"tokenFile"`
	// Token is the resolved access token, empty when none is configured or the panel is off.
	Token Secret `json:"-"`
	// LayoutFile is where the user's panel layout is saved. It need not exist yet; empty keeps the
	// layout in the browser only.
	LayoutFile string `json:"layoutFile"`
	// EventHistory, LogHistory and MemoryHistory are ring buffer lengths for the whole process,
	// not per account.
	EventHistory  int `json:"eventHistory"`
	LogHistory    int `json:"logHistory"`
	MemoryHistory int `json:"memoryHistory"`
}

// Account is one bot account. Every field except Username can come from defaults.
type Account struct {
	Username string `json:"username"`
	// PasswordEnv names the environment variable holding the password. {USER} expands to the
	// username upper-cased with anything but letters and digits turned into _.
	PasswordEnv string `json:"passwordEnv"`
	Enabled     bool   `json:"enabled"`
	// LogLevel overrides log.level for this account; after loading it is always set.
	LogLevel string `json:"logLevel"`
	// Script names the Go script the account runs, as rsb0t-scripts names it; empty connects the
	// account without a script, as cmd/fleet does today.
	Script string `json:"script"`
	// Settings is a JSON object the script decodes into its own struct with DecodeSettings.
	Settings json.RawMessage `json:"settings"`
	// The script starts StartDelay plus a random part of StartJitter after login, and stops after
	// MaxRuntime (0s runs until stopped).
	StartDelay  Duration  `json:"startDelay"`
	StartJitter Duration  `json:"startJitter"`
	MaxRuntime  Duration  `json:"maxRuntime"`
	State       State     `json:"state"`
	Reconnect   Reconnect `json:"reconnect"`
	// Password is resolved from PasswordEnv, the credentials file or a legacy sharedPassword; it
	// is empty for a disabled account.
	Password Secret `json:"-"`
}

// DecodeSettings strictly decodes the account's settings into v, rejecting keys v does not declare.
func (a Account) DecodeSettings(v interface{}) error {
	data := a.Settings
	if len(bytes.TrimSpace(data)) == 0 {
		data = json.RawMessage("{}")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("account %s: script %s: settings: %w", a.Username, a.Script, err)
	}
	return nil
}

// State limits what an account decodes and keeps. Nothing reads it yet; it is the switch the
// gamestate trackers are meant to honour.
type State struct {
	// ViewRadius is how far from the local player, in tiles, tracked entities are kept.
	// Why: the server's view distance is 15, so nothing beyond that ever arrives.
	ViewRadius int      `json:"viewRadius"`
	Trackers   Trackers `json:"trackers"`
}

// Trackers switches per-kind state tracking, all off unless the settings or the script turn one on.
type Trackers struct {
	Players     bool `json:"players"`
	NPCs        bool `json:"npcs"`
	GroundItems bool `json:"groundItems"`
	Locs        bool `json:"locs"`
	Chat        bool `json:"chat"`
}

// Reconnect is the per-account backoff after a dropped session or a failed login.
type Reconnect struct {
	Initial    Duration `json:"initial"`
	Max        Duration `json:"max"`
	Multiplier float64  `json:"multiplier"`
	// Jitter is the fraction of each delay randomised, 0 to 1.
	Jitter float64 `json:"jitter"`
	// MaxFailures consecutive failures stop the account; 0 retries forever.
	MaxFailures int `json:"maxFailures"`
}

func defaultGlobal() Global {
	return Global{
		Server: Server{
			Profile: liveProfile,
			Profiles: map[string]ServerProfile{
				liveProfile: {URL: "wss://w1.example.invalid/", HTTPBase: "https://w1.example.invalid/", RSAExponent: "65537"},
				// Why: a stock Engine-TS serves HTTP and the WebSocket on port 80 on Windows.
				"local": {URL: "ws://localhost/", HTTPBase: "http://localhost/"},
			},
			World:            1,
			Members:          true,
			AntiCheatPackets: true,
			DialTimeout:      Duration(15 * time.Second),
			ReadTimeout:      Duration(30 * time.Second),
		},
		Runtime: Runtime{MemoryLimit: "off", GCPercent: 100},
		Log:     Log{Level: "info", Output: "shared", Dir: "logs"},
		ControlPanel: ControlPanel{
			// Why: the same default internal/controlpanel uses, clear of cmd/fleet's 8090.
			Listen:        "127.0.0.1:7274",
			LayoutFile:    "controlpanel-layout.json",
			EventHistory:  256,
			LogHistory:    256,
			MemoryHistory: 300,
		},
	}
}

func defaultAccount() Account {
	return Account{
		Enabled: true,
		State:   State{ViewRadius: 15},
		Reconnect: Reconnect{
			Initial:     Duration(6 * time.Second),
			Max:         Duration(5 * time.Minute),
			Multiplier:  2,
			Jitter:      0.2,
			MaxFailures: 15,
		},
	}
}
