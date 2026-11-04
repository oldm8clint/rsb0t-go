# Subsystem diagrams

Source-checked 2026-09-24. These describe the ordinary control-panel fleet; see [architecture](architecture_overview.md) for process boundaries.

## Account packet and action path

```mermaid
flowchart TD
    Server[Game server] -->|WebSocket bytes| Session[client.Session read loop]
    Session -->|Decoded packets| State[gamestate.State]
    State --> Adapter[Registered bot adapter]
    Adapter -->|Snapshot| Plan[Pure script plan]
    Plan -->|Decision| Adapter
    Adapter --> Runner[Script runner helpers]
    Runner --> Actions[actions package]
    Actions --> Send[Session write path]
    Send -->|Encoded packets| Server
    State --> Guardian[Random-event guardian]
    Guardian --> Actions
    Guardian -.->|Busy and coordination gates| Adapter
```

Sources: [session](../internal/client/session.go), [bot](../internal/bot/bot.go), [fisher](../internal/bot/fisher.go), [guardian](../internal/randomevents). Sending an action is not proof of server acceptance: runners wait for state changes and apply timeouts.

## Runner lifecycle and pacing

```mermaid
flowchart LR
    Start[Runner.Start] --> OnStart[Bot.OnStart once]
    OnStart --> Check{Stopped or error?}
    Check -->|No| Loop[Bot.Loop]
    Loop --> Check
    Check -->|Yes| OnStop[Bot.OnStop and cleanup]
    Stop[Runner.Stop] -->|Cancel context waits| Check
```

[Runner](../internal/bot/runner.go) runs each bot in a goroutine. Stop requests cancellation; Wait waits for shutdown. Scripts own delays and tick waits, so there is no automatic one-Loop-per-tick contract. A non-cancellable blocking call can delay shutdown. Bot panics are reported by the runner; this is not isolation from every process failure.

## Fleet and panel

```mermaid
flowchart TD
    Config[Roster and launch flags] --> Main[cmd/controlpanel]
    Main --> Manager[fleet.Manager]
    Manager --> Routes[Routes and paced logins]
    Routes --> Sessions[Per-account sessions]
    Manager --> Bots[Per-account bot runners]
    Bots <--> Sessions
    Bots <--> Shared[Mule bus and bank directory]
    Manager --> Source[FleetSource]
    Fake[FakeSource] --> API[Panel HTTP API]
    Source --> API
    API <--> Browser[Browser panels]
```

Sources: [main](../cmd/controlpanel/main.go), [fleet](../internal/fleet), [FleetSource](../internal/controlpanel/fleetsource.go), [FakeSource](../internal/controlpanel/fake.go). FakeSource is an alternative, not another live roster. The browser does not own game sockets.

## Evidence boundary

```mermaid
flowchart LR
    Fisher[Opt-in fisher trace] --> Evidence[Bounded artifacts]
    Fixture[Offline scenario and policy] --> Evidence
    Evidence --> Eval[Replay and evaluation gates]
    Eval --> Review[Candidate report and review]
    Review -.->|Separate authorized change| Code[Script or settings revision]
```

Sources: [fisher](../internal/bot/fisher.go), [evaluation](../internal/ml/evaluation), [promotion](../internal/ml/promotion). The dotted revision step is an engineering workflow, not a deployed autonomous updater.
