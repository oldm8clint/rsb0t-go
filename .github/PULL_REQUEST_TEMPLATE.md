## What this changes

<!-- One thing, in a sentence or two. Link the issue if there is one. -->

## Kind of change

- [ ] New script (plan + runner + registration)
- [ ] Bug fix
- [ ] Wire protocol / game state
- [ ] Panel or setup page
- [ ] Proof tool under `cmd/`
- [ ] Documentation (including `docs/agents/`)

## How it was checked

- [ ] `go build ./...` exits 0
- [ ] `go vet ./...` exits 0
- [ ] `go test -p 1 ./...` exits 0
- Real-data packs set when I ran the tests? yes / no
- Run against a local engine? yes / no (revision: )
- What I did **not** run:

## Checklist

- [ ] It does one thing and the title says what.
- [ ] New behaviour has tests; a bug fix has a test that fails without it.
- [ ] Settings stay strict (unknown keys are errors) and `settings.example.json` / docs agree with the code.
- [ ] Protocol changes include the reference vector and its regenerated fixture.
- [ ] New scripts follow the plan / runner / registration split and log what they do and why.
- [ ] No credentials, personal data, addresses, private paths or real account names in the diff.
- [ ] Docs are updated; anything learned the hard way is in `docs/agents/`.
- [ ] I ran it, or I say above that I did not.

## Notes for the reviewer

<!-- Risks, trade-offs, things you are unsure about, screenshots for panel changes. -->
