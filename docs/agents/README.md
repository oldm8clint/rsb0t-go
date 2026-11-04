# Agent memory for rsb0t-go

This folder is the project's shared memory for AI coding agents and for the people who work with them. It is
meant to be read at the start of a task and added to at the end of one. [AGENTS.md](../../AGENTS.md) is the
short rulebook; these pages hold the detail and the reasons.

rsb0t-go is said "RS bot go".

| Page | Read it when |
|---|---|
| [architecture.md](architecture.md) | you need to know which package owns what, and how a packet or an action travels |
| [adding_a_script.md](adding_a_script.md) | you are writing or porting a script |
| [gotchas.md](gotchas.md) | something "does nothing", a login fails, or a number looks wrong |
| [protocol.md](protocol.md) | you touch bytes on the wire, opcodes, login or the cache |
| [testing.md](testing.md) | you want the real-data tests to run instead of skip, or you are writing a test |
| [local_engine.md](local_engine.md) | you need a game server of your own to test against |
| [lessons.md](lessons.md) | you want the engineering lessons behind the rules: how checks lie, how work collides, how machines get saturated |
| [open_work.md](open_work.md) | you want something to build |

Related pages elsewhere: [trustworthy_harnesses.md](../trustworthy_harnesses.md) (how to write a live proof tool
that can be believed), [script_porting_framework.md](../script_porting_framework.md) (the long form of the script
recipe), [fleet_config_reference.md](../fleet_config_reference.md) and
[bot_script_settings.md](../bot_script_settings.md) (configuration).

## How to add to this folder

- **Write down what cost you time.** A trap you fell into is a trap the next agent will fall into. Put it in
  [gotchas.md](gotchas.md) (a fact about the game, the server or the code) or [lessons.md](lessons.md) (a way of
  working), with the symptom first, because that is what the next reader searches for.
- **Say how you know.** "Proven against a local engine" and "read from the source" and "guessed" are three
  different strengths. Name which one, and name the source revision.
- **Keep it true.** If a change you make invalidates a line here, fix the line in the same pull request. A
  stale page is worse than a missing one.
- **Keep it free of secrets and of people.** No account names, passwords, tokens, addresses, private paths or
  logs. Write in general terms ("a server that rate-limits logins per address") rather than quoting a session.
- **Short beats complete.** A reader who has to scroll past ten paragraphs to find the symptom will not.
