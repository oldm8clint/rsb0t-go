# Developer handbook

Source-checked 2026-09-24. Start with the [repository map](README.md), [architecture](architecture_overview.md) and [contribution workflow](agent_contribution_guide.md).

## Setup and checks

[go.mod](../go.mod) declares Go 1.23. Use a compatible Go toolchain and Git. The headless client compiles without the sibling TypeScript project; local-engine proofs and optional renderers have additional inputs.

Run code gates separately and check each exit code:

```powershell
go build -p 1 ./...
go vet -p 1 ./...
go test -p 1 ./...
```

Package parallelism is limited for a shared machine. Start with affected packages for a small change, then run required full gates. These commands do not establish game-server proof.

## Simulated panel

```powershell
go run ./cmd/controlpanel -fake 8 -listen 127.0.0.1:7399 -market-prices=false
```

Choose an unused port, open http://127.0.0.1:7399 and stop this foreground command with Ctrl+C. Fake mode does not connect roster accounts to the game. It proves the simulated UI only; do not use the running fleet's port or launch another real roster beside it.

Real lab tests need a dedicated roster, local engine and matching cache/collision inputs. Read [fleet configuration](fleet_config_reference.md), [script settings](bot_script_settings.md), and the chosen harness's flags. Setup cheats belong only in the controlled lab; measured action phases must use the registered bot's action path.

## Locate behavior

| Question | Start here |
|---|---|
| Connection/retry problem | [client](../internal/client), [fleet](../internal/fleet) |
| Script decision or action | [porting framework](script_porting_framework.md), [bot](../internal/bot) |
| Unreachable tile | [pathfind](../internal/pathfind), [webwalk](../internal/webwalk), [locreach](../internal/locreach) |
| Panel data source | [Source](../internal/controlpanel/source.go), FleetSource and FakeSource |
| Required proof | [harness guidance](trustworthy_harnesses.md), [script parity](script_parity_status.md) |
| Older uncompleted tasks | [Luna audit](luna_task_status_2026-09-24.md), current COLLAB claims |

Use rg for symbols and rg --files for paths. Keep private passwords, tokens, raw chat and logs out of committed examples.

## Profiling

Measure a repeatable fixture before optimizing. For the offline replay benchmarks:

```powershell
go test ./internal/ml/replay -run '^$' -bench . -benchmem -cpuprofile replay.cpu.pprof
go tool pprof replay.cpu.pprof
```

The profile and generated test binary are local artifacts, not commit inputs. Compare the same benchmark and conditions before/after a change.

The panel accepts -pprof 127.0.0.1:6060 for explicit diagnostics. It is off by default and has no authentication of its own. Use a dedicated lab instance; changing live launch flags follows the fleet's operational workflow.

## Release record

No .github/workflows pipeline is checked in at this snapshot. CI/CD design documents do not establish that remote gates or automatic deployment ran.

Record the final commit, build/vet/test outcomes, local-engine evidence, candidate identity and limitations. Build candidates outside the live executable path. At authorized deployment follow the current handoff's graceful-stop, port-closure, log-preservation, cooldown and paced-login requirements, then verify the running revision. Do not reuse old PIDs or cooldown exemptions from historical notes.

After deployment report observed reconnect, progress and action outcomes over a stated window. Documentation changes need no restart. Experimental [session-host hot reload](../HOTRELOAD.md) does not yet replace this procedure for the ordinary fleet.
