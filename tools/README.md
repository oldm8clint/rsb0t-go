# Repository tools

Run commands from the repository root unless a tool's header says otherwise. This index groups existing tools without moving them or changing their command lines. For project priorities, use [current work](../docs/current_work.md); for architecture and topic references, use the [documentation index](../docs/README.md).

## Offline checks

These commands exercise local fixtures; they do not operate the live fleet. Python tests for fleet switching use fake panel requests, so their printed `APPLYING` messages describe fixtures.

```powershell
python -m unittest discover -s tools -p "test_*.py"
node --test tools/panel/incidents-scarab.test.mjs tools/panel/loot-merge.test.mjs
node tools/stream/verify-scene-state.mjs
```

Python test discovery covers fleet switching, progress watching, stall/handoff alarms and Scarab watching. Browser-based `verify-*.cjs` scripts are separate: several require an existing local harness, Edge and a sibling checkout's `playwright-core`. Read each script's header for its fixture URL and dependencies; a `verify` filename alone does not mean an offline test.

## Fleet observations and archived-log analysis

| Tool | Purpose / inputs |
|---|---|
| [fleet-audit.py](fleet-audit.py) | Read-only API/log evidence export to the required `--out` directory. |
| [fleet-watch.py](fleet-watch.py) | Record periodic fleet progress summaries. Use the alarm tools when a failing exit status is needed. |
| [fleet-stall-alarm.py](fleet-stall-alarm.py) | Detect running accounts that stop gaining skill XP. |
| [fleet-log-alarm.py](fleet-log-alarm.py) | Count trouble patterns that can occur while XP still rises. |
| [fleet-handoff-alarm.py](fleet-handoff-alarm.py) | Follow donor loads to delivery or failure outcomes. |
| [scarab-watch.py](scarab-watch.py) | Scarab phase/progress observations; supports archived-log replay. |
| [fleet-speed.py](fleet-speed.py) | Sample movement speed; its header explains sampling-interval bias. |
| [spot-metrics.py](spot-metrics.py) | Fishing-spot movement and delay measurements from a log. |
| [mule-throughput.py](mule-throughput.py) | Mule engagement time and capacity from plain or gzip logs. |
| [memory-benchmark.ps1](memory-benchmark.ps1) | Sample `/api/memory` into a CSV; requires `-BaseUrl`. |
| [panelperf/measure.cjs](panelperf/measure.cjs) | Browser rendering measurements; blocks non-GET requests and uses a local test layout. |

Read-only describes fleet interaction, not filesystem activity: exporters can write local reports. Logs and raw account observations belong in ignored local evidence directories; share scrubbed summaries in [docs](../docs/README.md).

## Fleet changes and local setup

| Tool | Effect |
|---|---|
| [fleet-switch.py](fleet-switch.py) | Plan, switch, retune or revert script assignments. Modes default to dry run; `--apply` enables POSTs and ledger changes. |
| [restore-parked-accounts.py](restore-parked-accounts.py) | Validate parked-account restoration and mule partner lists; `--apply` backs up and rewrites the roster. |
| [lostcity/bootstrap.ps1](lostcity/bootstrap.ps1), [bootstrap.sh](lostcity/bootstrap.sh) | Prepare the local Lost City checkout. |
| [lostcity/run-lab.ps1](lostcity/run-lab.ps1) | Start that checkout's engine; does not rewrite upstream source. |
| [ml-lab.ps1](ml-lab.ps1) | Launch the ML lab; default advisor is deterministic/offline, arguments pass through to `cmd/mllab`. |
| [tor/torrc.template](tor/torrc.template) | Proxy configuration template; local data and credentials are not source files. |

This index does not authorize a live rollout or roster change. Follow the owner's current direction and the tool's own usage contract.

## Rendering, browser probes and generated assets

| Directory / tool | Role |
|---|---|
| [capture](capture/) | Capture client, terrain/model helpers, sprite/font exporters, and fixture-based browser checks. |
| [stream](stream/) | Stream client, scene state/render/input modules, pinned-client manifests, browser checks and measurements. |
| [panel](panel/) | Panel fixture checks plus pure Node tests. |
| [panelperf](panelperf/) | Dashboard performance measurement harness. |
| [gen-untradeable.py](gen-untradeable.py) | Regenerate donor item metadata from a content checkout; writes `internal/scripts/donorplan/untradeable_gen.go`. |

The stream transport measurement opens/closes a server-side view lease even though it sends no game input. Input/walk probes can exercise controls. These differ from read-only API diagnostics; inspect their headers and use the intended lab fixture.

Root `*_reference_vector.ts` files are protocol/cache reference generators, not misplaced application files. Their paths remain intact for existing workflows. Likewise, `run/` contains operational launch scripts and local runtime state; this cleanup does not relocate either.
