package actions

import (
	"rsb0t-go/internal/archive"
	"rsb0t-go/internal/gamestate"
)

// MainModalTexts ports reader.mainModalTexts: every non-empty text component under the open
// main modal, in tree order. A component the server set with IF_SETTEXT shows that text (even
// when it set ""), otherwise the interface archive's own text, as the client keeps one text
// field that IF_SETTEXT overwrites.
func MainModalTexts(s Session, components archive.Components) []string {
	main, _, _ := s.State().Modals()
	if main == gamestate.NoModal {
		return nil
	}
	return modalTexts(s.State(), components, main)
}

func modalTexts(st *gamestate.State, components archive.Components, root int) []string {
	var out []string
	var walk func(id int)
	walk = func(id int) {
		com, ok := components[id]
		if !ok {
			return
		}
		if com.Type == archive.TypeText {
			text, set := st.TextKnown(com.ID)
			if !set {
				text = com.Text
			}
			if text != "" {
				out = append(out, text)
			}
		}
		for _, child := range com.Children {
			walk(child)
		}
	}
	walk(root)
	return out
}
