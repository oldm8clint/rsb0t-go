package actions

import (
	"strings"

	"rsb0t-go/internal/archive"
)

// ChatDialog is the ChatDialog.* surface (dialogue pages, option lists) from
// rsb0t-main's api, bound to a session and an already-parsed interface archive (the
// same one internal/actions.Bank uses — see NewBank's doc comment for how a real
// client wires this in once at startup). Ported from
// src/bot/api/ui/dialogue/ChatDialog.ts and the chatContinueComId/chatOptions readers
// behind it: both are plain component-tree scans over the open chat modal, not a
// dedicated protocol mechanic — "continue" is the same RESUME_PAUSEBUTTON this client
// already built for Trade/Bank's own pause buttons, and choosing an option is a plain
// IfButton click on that option's component id.
type ChatDialog struct {
	s       Session
	archive archive.Components
}

func NewChatDialog(s Session, components archive.Components) *ChatDialog {
	return &ChatDialog{s: s, archive: components}
}

func (d *ChatDialog) chatModal() int {
	_, _, chat := d.s.State().Modals()
	return chat
}

func (d *ChatDialog) IsOpen() bool {
	return d.chatModal() != -1
}

// continueComID finds a direct child of the open chat modal whose button type is
// BUTTON_CONTINUE — matches chatContinueComId's own single-level scan (not recursive:
// the "click here to continue" prompt is always a direct child of the modal root).
func (d *ChatDialog) continueComID() int {
	chat := d.chatModal()
	if chat == -1 {
		return -1
	}
	modal, ok := d.archive[chat]
	if !ok {
		return -1
	}
	for _, childID := range modal.Children {
		if child, ok := d.archive[childID]; ok && child.ButtonType == archive.ButtonContinue {
			return childID
		}
	}
	return -1
}

// CanContinue mirrors ChatDialog.canContinue: a continue prompt exists AND hasn't
// already been pressed for this interface open (the same one-press-per-open latch
// Trade/Bank's pause buttons use — see actions.ResumePauseButton).
func (d *ChatDialog) CanContinue() bool {
	if d.s.State().PauseButtonResumed() {
		return false
	}
	return d.continueComID() != -1
}

// Continue presses the "click here to continue" prompt.
func (d *ChatDialog) Continue() error {
	comID := d.continueComID()
	if comID == -1 {
		return ErrNotOnScreen
	}
	return ResumePauseButton(d.s, comID)
}

// Option is one selectable line in an open option-choice dialogue.
type Option struct {
	ComID int
	Text  string
}

// Options mirrors chatOptions: every BUTTON_OK component anywhere under the chat
// modal, in tree order — an option list nests its buttons inside layout components,
// unlike the continue prompt, so this scan is recursive.
//
// Each label is the text the server set with IF_SETTEXT when it has set one, as the TS
// client reads com.text after IF_SETTEXT has overwritten it. The archive's own text is only
// the fallback: the multi-choice chat interfaces (multi2..multi5) ship placeholder labels
// "option1", "option2", ... that the server replaces before opening them. A label the
// server cleared to "" is left out, as chatOptions leaves out an empty label.
func (d *ChatDialog) Options() []Option {
	chat := d.chatModal()
	if chat == -1 {
		return nil
	}
	st := d.s.State()
	var out []Option
	var visit func(comID int)
	visit = func(comID int) {
		com, ok := d.archive[comID]
		if !ok {
			return
		}
		if com.ButtonType == archive.ButtonOK {
			label, set := st.TextKnown(comID)
			if !set {
				label = com.Text
				if label == "" {
					label = com.ButtonText
				}
			}
			if label != "" {
				out = append(out, Option{ComID: comID, Text: label})
			}
		}
		for _, childID := range com.Children {
			visit(childID)
		}
	}
	visit(chat)
	return out
}

// ChooseOption presses the first option whose text contains match (case-insensitive),
// or the first option at all when match is "".
func (d *ChatDialog) ChooseOption(match string) error {
	opts := d.Options()
	if len(opts) == 0 {
		return ErrNotOnScreen
	}
	if match == "" {
		return IfButton(d.s, opts[0].ComID)
	}
	want := strings.ToLower(match)
	for _, o := range opts {
		if strings.Contains(strings.ToLower(o.Text), want) {
			return IfButton(d.s, o.ComID)
		}
	}
	return ErrItemNotFound
}
