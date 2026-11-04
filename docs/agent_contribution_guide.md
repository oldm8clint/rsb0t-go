# Contribution guide for agents and people

The contribution process is [CONTRIBUTING.md](../CONTRIBUTING.md). The rules an AI coding agent works to are in
[AGENTS.md](../AGENTS.md), and the detail behind them is in [agents/README.md](agents/README.md).

In one paragraph: fork, branch from the current upstream main, keep `go build ./...`, `go vet ./...` and
`go test -p 1 ./...` green and read each exit code, write the plan / runner / registration split for a script,
prove protocol changes against a reference vector, never put a credential anywhere in the repository, never point
a cheat-sending tool at a server you do not run, and write down what you learned in `docs/agents/`.
