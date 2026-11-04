# rsb0t-go documentation

rsb0t-go is said "RS bot go". Start at the [project README](../README.md); this page is the map of `docs/`.

## For people running it

| Page | What it covers |
|---|---|
| [getting_started.md](getting_started.md) | Run it, add an account, build from source |
| [fleet_config_reference.md](fleet_config_reference.md) | The roster file and the stricter version-1 settings file |
| [bot_script_settings.md](bot_script_settings.md), [bot_script_settings_additional.md](bot_script_settings_additional.md) | Every script's settings and defaults |
| [proxy_routes.md](proxy_routes.md) | Routing accounts through proxies, where the server's rules allow it |

## For people and agents changing it

| Page | What it covers |
|---|---|
| [../AGENTS.md](../AGENTS.md) | The short rulebook: commands, conventions, rules, where to look |
| [agents/README.md](agents/README.md) | The shared memory: architecture, adding a script, gotchas, protocol, testing, local engine, lessons, open work |
| [../CONTRIBUTING.md](../CONTRIBUTING.md) | Forks, branches, pull requests and the checklist |
| [architecture_overview.md](architecture_overview.md), [subsystem_diagrams.md](subsystem_diagrams.md) | Process shape and diagrams |
| [developer_handbook.md](developer_handbook.md) | Setup, checks, profiling, where to find behaviour |
| [script_porting_framework.md](script_porting_framework.md) | The long form of the plan / runner / registration recipe |
| [trustworthy_harnesses.md](trustworthy_harnesses.md) | How to write a live proof tool whose verdict can be believed |

Some pages were written inside a larger private project and link to notes that are not part of this
repository. If you find a link that goes nowhere, repoint it or remove it and send a pull request.
