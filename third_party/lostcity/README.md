# Lost City local dependency

This directory is reserved for a local checkout of the upstream Lost City server used by the rsb0t-go development/ML lab.

It is intentionally not populated in git. Run:

- PowerShell: `./tools/lostcity/bootstrap.ps1`
- POSIX shell: `./tools/lostcity/bootstrap.sh`

The bootstrap clones `https://github.com/LostCityRS/Server` into this directory. The upstream project is MIT licensed; retain its LICENSE and repository history in the local checkout.

Do not commit the cloned repository here. Treat it as an external development dependency. Keep rsb0t-go-specific adapters, fixtures and patches outside the checkout so upstream updates remain easy to reproduce.

## Intended ML use

The local world is a test environment for observation capture, replay, scenario fixtures and baseline-vs-candidate experiments. Start with read-only observation. Do not put LLM inference in the game tick path.
