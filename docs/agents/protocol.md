# Protocol notes

rsb0t-go speaks **client revision 289** of the RuneScape 2 protocol, over a WebSocket. Everything here was
established against LostCityRS Engine-TS and Content on branch 289 and the matching reference TypeScript client.

## The rule: the reference client is the authority

Bytes are right when they match what the reference client writes and reads. The files that define them in the
reference client are its `ClientProt.ts` (which sets `CLIENT_VERSION = 289`), `ClientStream.ts`, `Packet.ts`,
`Isaac.ts` and `Client.ts`. A comment in this repository that says a different revision is stale.

Protocol code is proven in two steps, in this order:

1. **Vectors captured from the real code.** A `*_reference_vector.ts` script at the repository root runs the
   reference implementation on fixed inputs and prints JSON; that JSON is committed under `test/fixtures/`; the
   Go tests compare byte for byte. Current vectors: `isaac`, `rsa`, `packet`, `opcode`, `userhash`, `wordpack`,
   `useon`, `lineofsight`, `webwalk`, `castle_collision`, `cachecfg`.
2. **Confirmed live against a local engine** ([local_engine.md](local_engine.md)). A vector shows your bytes
   equal the reference's; only a live run shows the server accepts them and does what you meant.

To regenerate a vector: `bun <name>_reference_vector.ts` from the repository root with the reference checkout
available (the scripts import a sibling checkout; fix the relative path for your layout). Never edit a fixture
to make a test pass. If a fixture and the code disagree, one of them is wrong and the reference decides.

A protocol change in a pull request includes: the vector script (new or updated), the regenerated fixture, the
Go test that compares against it, and a sentence saying whether it was also run live and against which engine
revision.

## Opcode tables are generated

`internal/protocol/clientprot` and `serverprot` come from `internal/protocol/gen`:

```sh
go run ./internal/protocol/gen -engine <path to an Engine-TS 289 checkout>
```

The generated `ServerProtSizes` array gives each server opcode's length rule (fixed, length-prefixed by one byte,
or by two). A wrong size desynchronises the whole stream, so a new server packet is added by regenerating, not
by hand. `diff_opcodes.ts` compares the tables with the reference. The generator has its own test that checks
its output against a ground-truth parse of the reference tables, to catch bugs in the generator itself.

## Login, in one page

`internal/login.PerformLogin` ports the byte sequence of the reference client's `performLogin`; it does not port
the retry state machine around it. Callers get the raw response code and decide.

1. Open the WebSocket (subprotocol `binary`).
2. Send the 2-byte session init (`14`, login-server byte); read the server's 8-byte seed.
3. Build the plaintext login block: a marker byte (`10` fresh, `18` reconnect), four int32 ISAAC seed words
   (two client-random, then the two halves of the server seed), the device id, then newline-terminated username
   and password. RSA-encrypt it (`internal/rsa`, textbook RSA with the server's modulus and exponent; often 65537, but a stock Engine-TS key's exponent is a
   77-digit number).
4. Wrap it in the outer login packet: opcode (`16` fresh, `18` reconnect), length, magic byte, client version
   (289), low-memory flag, and the **nine archive CRCs** that must equal the first 36 bytes of the server's
   `/crc0` (a mismatch is answered with response 6 before the RSA block is even read).
5. Read the response byte: `2` success, `15` reconnect success, anything else is a refusal
   ([gotchas.md](gotchas.md#login) lists the ones that matter).
6. Key two ISAAC ciphers: outgoing with the seed as is, incoming with every word plus 50. All later opcodes are
   encrypted with them.

The user hash that identifies an account is the reference client's (`userhash_reference_vector.ts`): usernames
that differ only in case or in `_` versus space are the same account, which is why duplicate detection compares
hashes.

## After login

- **Server to client** packets are read by `internal/client`, dispatched by `gamestate.State.Handle`. Interface
  and inventory packets use component ids from the interface archive; `internal/archive` parses it, and the
  engine's `interface.dat` and the client's packed `interface` are byte-identical for the same build.
- **Client to server** packets are built in `internal/actions`. Menu actions write the opcode and operands the
  reference client's `doAction` writes, and the **anti-cheat side packets** the reference client sends
  alongside (`ANTICHEAT_*`, counted by `internal/anticheat`). They are part of looking like a real client; they
  were checked against the engine's declared packet sizes and exercised live past their thresholds.
- **`CLIENT_CHEAT`** (`::` commands) is an opcode a real client only sends from a staff-only entry. Transmitting
  it is itself a fingerprint on a real server, whether or not the command is honoured. That is why cheats are
  gated by `actions.CheatEnabled` and refused by `internal/harness` for anything but a local engine.
- **Keepalive** is `NO_TIMEOUT` (181), sent by the session when it has been quiet.

## The cache

The client revision's data comes over HTTP from the server (`/crc0` for checksums, the archives for config and
interfaces). `cmd/cachecfgtest -http <base> -save config.jag` downloads the config archive. `internal/cachecfg`
turns it into object, NPC and loc definitions (names, options, sizes); `cmd/locbake` turns the engine's map data
into the static loc index. Two byte-level facts: archive blobs that are bzip2 have no stream header (prepend
`BZh1`), and inventory item ids on the wire are the object id plus one.

## Adding support for a packet

1. Find it in the reference client (`Client.ts` read/write, `ServerProt.ts`/`ClientProt.ts`) and in the engine's
   handler. Write down which file and revision.
2. If it is a new opcode, regenerate the tables rather than editing them.
3. Add a decoder in `gamestate` (server packets) or an action in `actions` (client packets).
4. Capture a vector or a real packet as a fixture. Write the test first if you can.
5. Run it against a local engine and say so in the pull request.
