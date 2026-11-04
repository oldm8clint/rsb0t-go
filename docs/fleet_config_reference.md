# Roster reference (`fleet.config.json`)

The roster is the file rsb0t keeps in `rsb0t-data/fleet.config.json`. `rsb0t setup`, `rsb0t server` and
`rsb0t account` write it; edit it by hand only for what they do not cover (per-account settings, data file
paths, worlds). [`fleet.config.example.json`](../fleet.config.example.json) shows the shape. The authoritative
loader is the `roster` struct in [`internal/controlpanelapp/main.go`](../internal/controlpanelapp/main.go).

**The roster never holds a password.** Passwords are in `credentials.json` beside it (named by
`credentialsFile`), written by the setup page or `rsb0t account add`, which asks for the password without
echoing it. Do not add a `password` or `sharedPassword` key, and never type a password on a command line.

## How it is read

- Plain `json.Unmarshal`: **an unknown or misspelt key is ignored, not an error.** After editing, read back
  what took effect (`rsb0t server`, `rsb0t account list`, the panel's start-up lines). The strict loaders are
  elsewhere: `settings.json` (`internal/config`) and each script's own `settings` object (`DecodeSettings`)
  reject unknown keys.
- The roster needs `wsURL`, `httpBase` (ending in `/`; added when missing) and at least one account, and every
  account needs a password in `credentials.json`; the panel stops with a message naming what is missing.
- A missing, zero or negative `loginDelaySeconds` becomes 3 seconds; fractions are allowed.
- Relative data paths (`packPath`, `locsPath`, `configPath`) are resolved from the working directory, which
  `rsb0t` sets to the data folder. Use forward slashes or absolute drive paths in JSON on Windows
  (`"C:/rsb0t/collision.lcnav.gz"`); a Git Bash path such as `/d/...` is not translated.

## Top-level keys

| Key | Type | What it is |
|---|---|---|
| `server` | string | The address as typed to `rsb0t server` or the setup page. Informational. |
| `wsURL` | string | The game WebSocket, e.g. `wss://w1.example-server.com/`. `rsb0t server` derives it (or takes `-game`). |
| `httpBase` | string | The web base the game files (`/crc0`) and web client (`client/client.js`) hang off, ending in `/`. |
| `credentialsFile` | string | The passwords file, relative to the roster: `credentials.json`. |
| `loginDelaySeconds` | number | Seconds between login starts (default 3). Every login also goes through one fleet-wide rate gate. |
| `modulus`, `exponent` | string | The login key in decimal, only when it cannot be read from `client.js` (see below). |
| `packPath`, `locsPath`, `configPath` | string | The data files walking scripts need: collision pack, loc index, config archive. Optional. |
| `worldList` | array of 1-255 | The worlds the server has. `rsb0t server <one world>` fills it with those that answer. |
| `worlds` | object | Hand-written endpoints for worlds whose hosts are not `wN.<site>`: `{"2": {"wsURL": "...", "httpBase": "..."}}`. |
| `browserHeaders` | boolean | Open the game WebSocket with browser-like headers (an Origin). `rsb0t server` sets it when the server wants it. |
| `deviceIds` | string | `"fixed"` (default: the web client's constant id) or `"per-account"`. |
| `antiCheat` | boolean | Omitted or true sends the web client's ANTICHEAT side-packets; false stops them. |
| `crcTable` | array | Nine archive checksums to send instead of fetching `/crc0`. Leave it out. |
| `pinRoutes` | boolean | With `-proxies`, tie each account to one route for good. |
| `accounts` | array | The accounts, below. |

## Account keys

| Key | Type | What it is |
|---|---|---|
| `username` | string | The login name, up to 12 letters, digits, spaces or underscores. |
| `script` | string | A registered script's exact name (`rsb0t scripts`); empty logs in and does nothing else. |
| `world` | integer | 0 or absent: the world `wsURL` names. N: world N (from `worldList` or `worlds`). |
| `settings` | object | The script's own settings; see [bot_script_settings.md](bot_script_settings.md). Strict: an unknown key stops the script with an error. |

## The login key

With no `modulus`, the panel reads the key from `<httpBase>client/client.js` at every start: a
`BigInt("<modulus>"),BigInt("<exponent>")` pair with a modulus of 100 digits or more (a stock Engine-TS 289
bundle carries a 154-digit modulus and a 77-digit exponent), or a run of 250+ digits with exponent 65537 unless
the bundle states another. It reads it again after a response 6, in case the server restarted with a new key.

Set `modulus` (and `exponent`; empty means 65537) only when that file does not carry the key the server
decrypts with, for example for a local engine (its `data/config/private.pem`). `rsb0t server ADDRESS -modulus
N -exponent N` writes them. A key set here is used as it is and **never re-read**: if that server changes its
key, every login is refused with response 6 until the new one is set.

## Worlds

A world is a different `wsURL`: the login block carries no world number. Endpoints are derived by replacing a
leading `wN.` host label; a host without one is used as it is, so `worldList` alone does not create a second
server. An explicit `worlds` entry overrides derivation (an omitted half is derived; both blank is an error).
Each world is checked with one read-only GET of its `/crc0` at start; one that does not answer is reported as
not available and the fleet starts anyway. Choose each account's world with `world`; an account refused with
response 7 (world full) is moved to a listed world with room.

## Saved script assignments

A script started from the panel is remembered in `script-assignments.json` in the data folder and wins over
the roster's `script` at the next start. `GET /api/assignments` shows them; `DELETE
/api/assignments?account=NAME` makes that account follow the roster again.

## Adding an account while the panel runs

The panel has a runtime add-account API whose request carries a `password`. It is for the panel's own pages.
Do not call it from a command line (`curl ... password=...`): the password lands in shell history and
transcripts. Use the setup page or `rsb0t account add`, then restart.

## A first roster

`rsb0t server <address>` and `rsb0t account add NAME` produce this; it logs one account in and runs nothing:

```json
{
  "server": "https://example-server.com",
  "wsURL": "wss://w1.example-server.com/",
  "httpBase": "https://w1.example-server.com/",
  "credentialsFile": "credentials.json",
  "loginDelaySeconds": 3,
  "worldList": [],
  "accounts": [
    { "username": "YourAccount", "script": "", "world": 0, "settings": {} }
  ]
}
```

When a key, field or behaviour changes, update the loader, `fleet.config.example.json` and this page together.
