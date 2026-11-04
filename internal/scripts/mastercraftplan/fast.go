package mastercraftplan

import (
	"rsb0t-go/internal/scripts/gemplan"
	"rsb0t-go/internal/scripts/levels"
)

// THE FAST STREAM (a user request: maximum XP an hour, a packet pattern no real client sends
// accepted). A load is a fixed run of user events: the bank events (Deposit-All of the last load's
// product, the withdrawals), the uses, sometimes a filler, and the booth's Use-quickly that opens the
// bank for the next load. The engine reads five handled user events a player a tick and leaves the
// rest in its socket buffer for the next tick (engine/src/engine/entity/NetworkPlayer.ts decodeIn),
// so a load of 5n events is exactly n ticks when the stream is sent ahead and every tick finds five
// waiting. What has to line up, all checked against engine 289 and content 289:
//
//   - The booth op must be the LAST event of its tick. The bank opens in that tick's player turn
//     (World.processPlayers -> processInteraction, after processClientsIn read the packets), and a
//     deposit or withdrawal read before then is refused (InvButtonHandler: isComponentVisible) and,
//     being refused, does not count towards the five, so the tick would read on into the uses.
//     After the booth comes the next tick's bank events, then the uses. So a load's events are a
//     multiple of five, filled out with handled events where they fall short (FastStream.Pad).
//   - A withdrawal's items are in the pack for an OPHELDU read later in the same tick: the
//     INV_BUTTON handler runs the bank script at once (bank.rs2 bank_withdraw_request ->
//     inv_moveitem_uncert -> engine Inventory.remove/add), so the uses can follow in the same tick.
//     A Withdraw-X stops the script at its count dialog (engine PlayerOps P_COUNTDIALOG) and the
//     RESUME_P_COUNTDIALOG behind it resumes it in the same read (ResumePCountDialogHandler).
//   - No close is needed: the first use calls clearPendingAction, which closes the bank
//     (OpHeldUHandler; Player.closeModal), and a level-up box the same way.
//   - The uses name the slots the withdrawals fill: Deposit-All empties every slot but the tool's
//     (bank_deposit: inv_total), a withdrawal fills the empty slots lowest first (engine
//     Inventory.add), and a use removes the lowest slot holding the item (Inventory.remove) and puts
//     its product in the lowest empty slot. The runner maps a use's index to its slots.
//   - Every chunk of five goes out as ONE websocket message, which the engine buffers whole
//     (web.ts: client.buffer(message)), so a tick never reads part of a chunk.
//
// A level-up opens a chat box from the engine queue, which runs before the interaction in the
// player's turn (Player.processEngineQueue, then processInteraction, which needs no box open:
// canAccess). A level gained by a use in a booth's tick would therefore keep the bank shut, and the
// whole load after it would be refused. FastStream.Loads ends a run before such a load.

// FastKind is one event of the stream.
type FastKind int

// The events.
const (
	FastDeposit FastKind = iota
	FastWithdraw
	FastUse
	FastBooth
	// FastWithdrawX is a Withdraw-X (INV_BUTTON5) and FastResume its count (RESUME_P_COUNTDIALOG).
	FastWithdrawX
	FastResume
	// FastPad is a handled event that changes nothing (a CLOSE_MODAL read before the booth's
	// interaction: Player.processQueues closes before processInteraction opens).
	FastPad
)

// FastEvent is one user event: its kind, the load it belongs to, its place among the load's head
// events (Head) or uses (Use), and, for the chisel stream, the pack slot a use or the deposit names.
type FastEvent struct {
	Kind      FastKind
	Load      int
	Head, Use int
	Slot      int
}

// FastChunk is the events a tick.
const FastChunk = 5

// FastStream is the shape of one load: the head events in order (the first is the deposit), the
// uses, the fillers, then the booth op.
type FastStream struct {
	Head []FastKind
	Uses int
	Pad  int
}

// ChiselStream is the chisel's load (gems, oyster pearls): Deposit-All, Withdraw-All, 27 uses (every
// slot but the chisel's), the booth op: 30 events, six ticks.
var ChiselStream = FastStream{Head: []FastKind{FastDeposit, FastWithdraw}, Uses: gemplan.PackSlots - 1}

// BowStream is a bow-stringing load: three bank events, 14 uses, two fillers and the booth op: 20
// events, four ticks. The bank events are the owner's order (6 Oct 2026: "after the first withdraw of
// 14 bows 14 bow string, when it banks it should withdraw all bow string, bank the strung bows,
// withdraw all unstrung bows ... saving an input after the first cycle of stringing starts"):
//
//   - Every load but the first starts with the last load's 14 strung bows in slots 0 to 13. Withdraw-All
//     of the strings (bank.rs2 bank_withdraw_request: as many as the pack has room for) fills exactly
//     the 14 free slots, 14 to 27; Deposit-All of the strung bows empties slots 0 to 13; Withdraw-All of
//     the unstrung bows fills those 14. Each is one packet and none opens a count dialog.
//   - The first load of a run starts from an empty pack, where Withdraw-All of the strings would fill
//     all 28 slots, so it draws the 14 bows with Withdraw-X and its count, then Withdraw-All of the
//     strings (the 14 slots left): the same three events, which is why the stream is one shape for every
//     load (the runner says which events a load's three are).
//
// The old head was Deposit-All, Withdraw-X with its RESUME_P_COUNTDIALOG, Withdraw-All: four events and
// a count dialog every load, one filler. Three events and two fillers is the same four ticks, with a
// packet less a load and no dialog.
var BowStream = FastStream{Head: []FastKind{FastWithdraw, FastDeposit, FastWithdraw}, Uses: gemplan.PackSlots / 2, Pad: 2}

// LoadEvents is a load's events.
func (s FastStream) LoadEvents() int { return len(s.Head) + s.Uses + s.Pad + 1 }

// Valid reports whether a load fills whole ticks, so the booth op is the last event of its tick.
func (s FastStream) Valid() bool { return s.Uses > 0 && s.LoadEvents()%FastChunk == 0 }

// Ticks is a load's ticks.
func (s FastStream) Ticks() int { return s.LoadEvents() / FastChunk }

// Events is how many events a run of loads sends: every load's, less the booth op of the last (the
// run ends with the bank shut and the last load's product in the pack).
func (s FastStream) Events(loads int) int {
	if loads < 1 {
		return 0
	}
	return loads*s.LoadEvents() - 1
}

// Chunks is how many chunks a run of loads sends.
func (s FastStream) Chunks(loads int) int { return (s.Events(loads) + FastChunk - 1) / FastChunk }

// EventAt is the i-th event of a run.
func (s FastStream) EventAt(i int) FastEvent {
	L := s.LoadEvents()
	pos := i % L
	ev := FastEvent{Load: i / L, Head: -1, Use: -1}
	switch {
	case pos < len(s.Head):
		ev.Kind, ev.Head = s.Head[pos], pos
	case pos < len(s.Head)+s.Uses:
		ev.Kind, ev.Use = FastUse, pos-len(s.Head)
	case pos < L-1:
		ev.Kind = FastPad
	default:
		ev.Kind = FastBooth
	}
	return ev
}

// ChunkAt is chunk k of a run of loads: up to five events, sent as one message.
func (s FastStream) ChunkAt(k, loads int) []FastEvent {
	var out []FastEvent
	for i := k * FastChunk; i < (k+1)*FastChunk && i < s.Events(loads); i++ {
		out = append(out, s.EventAt(i))
	}
	return out
}

// UsesIn is how many uses the first chunks of a run of loads hold.
func (s FastStream) UsesIn(chunks, loads int) int {
	n := min(max(chunks, 0)*FastChunk, s.Events(loads))
	L := s.LoadEvents()
	return n/L*s.Uses + min(max(n%L-len(s.Head), 0), s.Uses)
}

// BoothUses is how many uses are read in the booth's tick: the last ones of the load.
func (s FastStream) BoothUses() int {
	L := s.LoadEvents()
	n := 0
	for pos := L - FastChunk; pos < L-1; pos++ {
		if pos >= len(s.Head) && pos < len(s.Head)+s.Uses {
			n++
		}
	}
	return n
}

// Loads is how many loads a run may make, at most want: none past a load whose booth op would share
// its tick with a level gained (see the package comment above), which is then the run's last load
// and has no booth op. xp is the skill's displayed XP (a floor of content's tenths, so a level may
// come one use sooner than the arithmetic says: both are guarded) and useXP a use's experience in
// content tenths.
func (s FastStream) Loads(want, xp, useXP int) int {
	if want < 1 || useXP <= 0 || !s.Valid() {
		return 0
	}
	booth := s.BoothUses()
	danger := func(c int) (load int, bad bool) {
		if c < 1 || booth == 0 {
			return 0, false
		}
		local := (c - 1) % s.Uses
		return (c - 1) / s.Uses, local >= s.Uses-booth
	}
	for level := levels.LevelFromXP(xp) + 1; level <= levels.MaxLevel; level++ {
		need := levels.XPAtLevel(level)*10 - xp*10
		c := (need + useXP - 1) / useXP // the use that reaches the level
		if (c-2)/s.Uses >= want {
			break
		}
		for _, cc := range []int{c - 1, c} {
			if load, bad := danger(cc); bad && load < want {
				return load + 1
			}
		}
	}
	return want
}

// ---- the chisel stream, by slot (the gem and pearl runs, and their tests) ----

// FastLoadEvents is the chisel load's events.
const (
	FastLoadEvents = 30
	// FastLoadUses is a chisel load's uses: every pack slot but the chisel's.
	FastLoadUses = gemplan.PackSlots - 1
)

// FastEvents is ChiselStream.Events.
func FastEvents(loads int) int { return ChiselStream.Events(loads) }

// FastChunks is ChiselStream.Chunks.
func FastChunks(loads int) int { return ChiselStream.Chunks(loads) }

// packSlot is the nth pack slot that is not the chisel's.
func packSlot(n, chiselSlot int) int {
	if n >= chiselSlot {
		return n + 1
	}
	return n
}

// FastEventAt is the i-th event of a chisel run, with its slot: Deposit-All names the first slot
// that is not the chisel's, the uses every such slot ascending.
func FastEventAt(i, chiselSlot int) FastEvent {
	ev := ChiselStream.EventAt(i)
	switch ev.Kind {
	case FastDeposit:
		ev.Slot = packSlot(0, chiselSlot)
	case FastUse:
		ev.Slot = packSlot(ev.Use, chiselSlot)
	}
	return ev
}

// FastChunkAt is chunk k of a chisel run, with slots.
func FastChunkAt(k, loads, chiselSlot int) []FastEvent {
	out := ChiselStream.ChunkAt(k, loads)
	for j := range out {
		out[j] = FastEventAt(k*FastChunk+j, chiselSlot)
	}
	return out
}

// FastUsesIn is ChiselStream.UsesIn.
func FastUsesIn(chunks, loads int) int { return ChiselStream.UsesIn(chunks, loads) }

// FastLoads is ChiselStream.Loads.
func FastLoads(want, xp, gemXP int) int { return ChiselStream.Loads(want, xp, gemXP) }

// FastCutsSeen is how many uses an XP gain in displayed points stands for.
func FastCutsSeen(gained, useXP int) int {
	if useXP <= 0 || gained <= 0 {
		return 0
	}
	return (gained*10 + useXP/2) / useXP
}
