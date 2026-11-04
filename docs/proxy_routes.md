# Proxy routes: running more accounts than one address allows

> **[2026-09-21] The MECHANISM below is correct and current. The NUMBERS are a dated snapshot.**
> This page describes two proxy routes, 266 accounts and an 89/89/88 split. The route count is
> read from a pool file at runtime (`internal/fleet/routes.go:209`) and is not fixed in code, so
> any route count or account count in this repository is a snapshot of somebody's pool file on
> some day, never a contract. The one durable number is the per-address cap:
> `internal/fleet/routes.go:57`, `DefaultRouteMax = 89`. Treat the worked examples here as
> examples. See [`docs/README.md`](README.md#the-live-production-fleet).


A server can cap how many accounts one address keeps logged in. The live server's rule is **at most 90 accounts logged in at once per IP address**. The fleet can log the rest in through free local proxies, each of which gives the server a different address.

**Use this only where the server's rules and its owner allow it.** Check your own server's rules before turning it on.

With no pool file, nothing changes: the fleet logs in from your own address as before.

## Quick start (Windows)

1. **Cloudflare WARP (free).** Install the WARP client from <https://1.1.1.1/> or with `winget install Cloudflare.Warp`.
2. **Psiphon (free).** Download the Windows app from <https://psiphon.ca/en/download.html> and put `psiphon3.exe` in `run\psiphon\`. That folder is gitignored. You can keep it elsewhere and pass `-PsiphonExe <path>` or set `RSB0T_PSIPHON_EXE`.
3. **Run the setup once:**

   ```powershell
   powershell -NoProfile -ExecutionPolicy Bypass -File run\setup-proxies.ps1
   ```

   The script:
   - Puts WARP in local proxy mode on `127.0.0.1:40000` and connects it.
   - Sets Psiphon's own settings so it never changes the Windows system proxy and never opens a browser, with its local SOCKS proxy on `127.0.0.1:40100` and a US exit.
   - Starts Psiphon minimised.
   - Writes `run\proxies.json` (gitignored) listing the routes that came up.
   - Checks that the Windows system proxy has not changed.

   It installs and downloads nothing. If an app is missing it says so and sets up the rest.
4. **Start the fleet as usual.** When `run\proxies.json` exists, `start-tested-after-cooldown.ps1` does two things:
   - Adds `-proxies proxies.json` to the fleet command.
   - Starts `run\proxy-keepalive.ps1` hidden. Every 30 seconds it reconnects WARP if its port is down and restarts Psiphon if its window was closed, and it exits when the fleet stops.

Your normal browsing is untouched. Both apps only carry traffic that is pointed at their local ports, and the only program pointed there is the fleet.

## What happens at login

- **Fill order:** accounts take the home address first, up to its `max` (89), then WARP, then Psiphon. No route ever exceeds its `max`. Accounts that fit nowhere wait queued and send nothing.
- **Pacing:** every route paces its own first logins side by side, so the whole fleet ramps at once, like players on different addresses.
- **Login bursts:** the server refuses the 30th login attempt from one address inside a rolling 60 seconds (Engine-TS `loginAddressAttempts`, response 16), however slowly the attempts arrive. So each route makes 28 attempts, reconnects included, and then rests 65 seconds. On the live fleet, 266 accounts were in game about six minutes after launch with zero refusals.
- **Per-route problems:** a response 16 pauses only that route, and a response 9 ("address full") holds new logins on that route alone.
- **Failover:** a proxy that stops answering is marked unhealthy with a backoff. Its waiting accounts move to another route or queue, and accounts already playing on it are left alone.
- **Sticky routes:** an account keeps its route across reconnects while that route is healthy.
- **Latency gate:** before a route takes anyone, it measures connect plus TLS to the game host (median of 5 samples) against the route's `maxLatencyMs`, which keeps slow routes unused.

The **Logins** panel shows each route's accounts in game, its status (logging in, resting with a countdown, full or unhealthy), logins that succeeded or failed, drops and latency. **Accounts** has a Route column. The raw counters are under `/api/status` → `logins.routes`.

## Measured on the live fleet (19 September 2026, Perth)

| Route | Round trip to the game | Accounts | Median fishing XP/h |
|---|---|---|---|
| Home (direct) | ~546 ms | 89 | 13.8k |
| Cloudflare WARP | ~545 ms, the same as direct | 89 | 14.1k |
| Psiphon, US exit | ~626 ms, about 80 ms more | 88 | 13.2k |

- **Psiphon's exit region matters:** left on automatic it picked Toronto, which added about 0.7 seconds per round trip.
- **Held connections:** a real game connection held 25 seconds idle through both proxies without being cut. WARP's documented 10-second request limit does not apply to these connections.
- **Tor** was measured at 0.8–1.0 seconds extra per round trip on every exit tried, so its template route stays disabled.

## The pool file

`proxies.example.json` shows every field. Routes are tried in file order.

```json
{
  "direct": {"max": 89},
  "routes": [
    {"id": "warp", "type": "socks5", "addr": "127.0.0.1:40000", "max": 89, "dns": "local", "latencyGate": true, "maxLatencyMs": 1500},
    {"id": "psiphon", "type": "socks5", "addr": "127.0.0.1:40100", "max": 88, "dns": "local", "latencyGate": true, "maxLatencyMs": 2500}
  ]
}
```

- **`type`:** `socks5` or `http` (HTTP CONNECT). TLS to the game runs end to end through the tunnel.
- **`dns`:**
  - `local` resolves the game host on this machine and sends the proxy an address. WARP requires this: it refused every by-name request in testing.
  - `remote` lets the proxy resolve the name.
- **Credentials:** a route can carry `username` with `password` or `passwordEnv` (an environment variable name). Passwords print as `[redacted]` and the file is gitignored; never commit one.
- **`enabled: false`** keeps a route in the file without using it.
- **Address limit:** set `max` to at most the server's per-address limit minus one. One route is assumed to be one exit address, and WARP's and Psiphon's addresses are shared and can change. A response 9 on a route is handled, not prevented.

## Troubleshooting

| Symptom | Check |
|---|---|
| A route stays at 0 with "latency check failed" | Its connect-plus-TLS median is above `maxLatencyMs`. For Psiphon, pin a nearer exit (`-PsiphonRegion` on the setup script) and restart it. |
| A route is marked unhealthy | Its app is not listening. Check `warp-cli status`, or whether Psiphon is running. The keep-alive log is `run\proxy-keepalive.log`. |
| Accounts sit "queued for a route" | Every route is full or resting. This is expected past the sum of the routes' `max` values. |
| Normal browsing changed | Should not happen with these settings. Check Settings > Network > Proxy; the setup script reports any change it saw. |
