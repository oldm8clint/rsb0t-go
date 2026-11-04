# Getting started with rsb0t-go

rsb0t-go is said "RS bot go". The program you run is `rsb0t`. It plays on any RuneScape 2 **revision 289**
server, such as one built on the LostCity engine (Engine-TS 289). Other revisions will not work. Use it only on
accounts you own, and only where the server's rules allow automation.

## Set it up (no programming needed)

1. **Download** the file for your computer from the [Releases page](https://github.com/oldm8clint/rsb0t-go/releases)
   (no release yet? see "Build and test from source" below) and **unpack** it (right-click, *Extract All* on
   Windows; double-click on macOS). You get a folder with `rsb0t` in it.
2. **Double-click `rsb0t`** (`rsb0t.exe` on Windows). A black window opens: that is the program. Leave it
   open. Your browser opens the **rsb0t-go setup** page; if it does not, go to <http://127.0.0.1:7274/>.
3. **Your game server.** Type the address of the server's website, or of the page where you play, for example
   `https://example-server.com`, and press **Check connection**. Each check shows **OK**, **Note** or
   **Problem**, and a sentence at the bottom says whether the server can be used. When it says **Ready**, press
   **Use this server**.
   - *"Nothing answered"*: check the spelling, and that the server is online.
   - *"does not look like a LostCity revision-289 game server"*: try the address of the page where you play,
     or a world address like `w1.example-server.com`.
   - *"needs its login key"*: ask the server's staff for its login public key (two numbers), paste them in
     the key boxes that open, and press **Use this server** again.
   - *"is not its game connection"*: ask the server's staff for the game's address (it starts with `wss://`)
     and type it in the **Game connection address** box that opens.
4. **Add an account.** Type its login name and password. Use only your own accounts, with the name typed
   exactly: on some servers a login with a new name creates that account. The password stays on this computer
   and is never shown again. Leave **Script** on *(none: just log in)* the first time and press **Add**.
   Later, choose a script in the account list; **Settings** next to it opens a short form. Scripts marked
   "needs map data" walk and will not start without a collision pack, which this download does not include.
5. **Press Start the panel.** The page becomes the panel, one card per account. Each card shows the account
   logging in, then what it is doing, and a plain-English message if something is wrong.

To stop, close the black window (or press `Ctrl+C` in it); every account logs out. Next time, double-click
`rsb0t` and it goes straight to the panel.

To bring the setup page back, or to see the demo (a pretend fleet that connects to nothing), open a command
window in the folder and type `rsb0t setup` or `rsb0t demo` (on macOS and Linux, `./rsb0t setup`). On Windows:
open the folder, click its address bar, type `cmd`, press Enter, then type the command. On macOS: open
Terminal, type `cd ` and drag the folder in, press Enter, then type the command.

## The same with commands

```sh
rsb0t check-server https://example-server.com   # read-only: is this a server rsb0t can use?
rsb0t server https://example-server.com         # check it and save it in rsb0t-data/fleet.config.json
rsb0t account add MyAccount                     # asks for the password (not echoed)
rsb0t account script MyAccount Alcher           # optional
rsb0t                                           # starts the panel on http://127.0.0.1:7274/ and opens it
```

`rsb0t help` lists every command; `rsb0t server -h` explains `-game`, `-modulus` and `-exponent`. Data
(roster, passwords, saved panel state) is in `rsb0t-data/`. `rsb0t server` saves an address whose game
files answered even when another check failed (so you can add accounts meanwhile); it then exits 1 and says
what to fix before the first start.

### What "check the server" does

It never logs in and sends no password. It asks the server's web address for four things, in order:

1. **Reachable**: does anything answer at that address?
2. **Game files (`/crc0`)**: every LostCity server publishes its nine archive checksums (a small file that
   tells the client which version of the game data the server has) at `/crc0`; a login carries them. No
   `/crc0` means it is not that kind of server, or the game lives on another address: when the website has
   none, `w1.` in front of it (without any `www.`) is tried too. A redirect (http to https, say) is followed
   and the address it ends at is used.
3. **Login key**: the key the server locks passwords with (an RSA key: two numbers, the "modulus" and the
   "exponent"), read from its web client (`/client/client.js`). A stock Engine-TS checkout's key and a
   regenerated one are both found. If a server hides it, ask its staff for the login public key and paste it.
4. **Game connection**: the game WebSocket (the live connection the game runs over) opens and is closed
   straight away. A server that only accepts browsers gets browser headers turned on for it. If the web server
   answers there instead, the game is on another address: give it with **Game connection address** or `-game`.

When the address names a world (`w1.example-server.com`), worlds `w1` to `w10` are tried and the ones that answer
are listed. The revision itself is only known at the first login: a server on another revision answers every
login with **response 6**, and no setting fixes that.

## Build and test from source

You need Git and Go 1.23 or newer (the floor in `go.mod`).

```sh
git clone https://github.com/oldm8clint/rsb0t-go.git
cd rsb0t-go
go build ./...
go vet ./...
go test -p 1 ./...
go run ./cmd/rsb0t demo
```

`-p 1` keeps a laptop responsive; the suite is large. Tests that need the generated game data
(collision pack, loc index, config archive) skip themselves when you have not built it.

## Where things are

| Path | What |
|---|---|
| `cmd/rsb0t` | the program: setup page, server check, accounts, panel, demo |
| `internal/serverprobe` | turns a server address into endpoints and checks the server, read-only |
| `internal/controlpanel`, `internal/controlpanelapp` | the panel server and the program that wires it to the fleet |
| `internal/fleet` | logins, pacing, routes, the watchdog, the login key |
| `internal/client`, `internal/protocol`, `internal/packet` | the wire protocol (revision 289) |
| `internal/gamestate`, `internal/actions` | what the game says, and what an account can do |
| `internal/bot`, `internal/scripts` | the scripts: a pure plan, a live runner, a registration |
| `test/fixtures` | byte-for-byte vectors captured from the reference client |

Passwords never go in the roster, in source control or in a script setting. See
[fleet_config_reference.md](fleet_config_reference.md) for the roster and
[script_porting_framework.md](script_porting_framework.md) to write a script. Working with an AI coding agent?
Point it at [AGENTS.md](../AGENTS.md) and [agents/README.md](agents/README.md); its "Set it up for a user"
section is a step-by-step an agent can follow.
