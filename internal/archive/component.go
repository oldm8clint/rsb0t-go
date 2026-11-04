package archive

import "rsb0t-go/internal/packet"

// Component types this client cares about distinguishing; the rest are read (to stay
// byte-aligned) but not specially handled. Values from IfType.ts's ComponentType enum.
const (
	TypeLayer   = 0
	TypeUnused  = 1
	TypeInv     = 2
	TypeRect    = 3
	TypeText    = 4
	TypeGraphic = 5
	TypeModel   = 6
	TypeInvText = 7
)

// Button types this client checks for (targetVerb/buttonText field presence).
const (
	ButtonOK       = 1
	ButtonTarget   = 2
	ButtonClose    = 3
	ButtonToggle   = 4
	ButtonSelect   = 5
	ButtonContinue = 6
)

// Component is the subset of IfType's fields a state-reading (not rendering) client
// needs: enough to walk the tree (LayerID, Children) and identify a bank/inventory
// component (Type, Iop) or read a label (Text).
type Component struct {
	ID         int
	LayerID    int
	Type       int
	ButtonType int
	Children   []int
	Iop        [5]string // item-operation labels; "" means absent (matches iop[i]===null)
	Text       string
	ButtonText string // BUTTON_OK/TOGGLE/SELECT/CONTINUE's own label — chat options and make-panels read this, not Text
	ObjOps     bool   // TYPE_INV/TYPE_INV_TEXT: whether item options are enabled at all
	ObjUse     bool   // TYPE_INV: whether the game offers "Use" on its items
	ClientCode int    // non-zero: the game client handles the button itself (logout, friends, ...)
}

// Components indexes parsed interface definitions by id, mirroring IfType.list.
type Components map[int]*Component

// ParseInterfaces ports IfType.init's "data" file parser: a flat, positional binary
// format where every component's fields must be read in the exact order and count the
// original writer used, or every component after a misread one desyncs. Fields this
// client has no use for (graphics, models, fonts, colours, scripts) are still read in
// full — just discarded — to keep the cursor aligned; see the inline comments matching
// each block back to IfType.ts's own line-by-line source.
func ParseInterfaces(data []byte) Components {
	r := packet.NewReader(data)
	out := make(Components)
	layer := -1

	_ = r.G2() // count: only used by the original to presize an array; irrelevant here.

	for r.Available() > 0 {
		id := int(r.G2())
		if id == 65535 {
			layer = int(r.G2())
			id = int(r.G2())
		}

		com := &Component{ID: id, LayerID: layer}
		out[id] = com

		com.Type = int(r.G1())
		com.ButtonType = int(r.G1())
		com.ClientCode = int(r.G2())
		width := int(r.G2())
		height := int(r.G2())
		_ = r.G1() // trans

		overLayerHi := int(r.G1())
		if overLayerHi != 0 {
			_ = r.G1() // overLayerId low byte; combined value unused here
		}

		scriptStackCount := int(r.G1())
		for i := 0; i < scriptStackCount; i++ {
			_ = r.G1() // scriptComparator
			_ = r.G2() // scriptOperand
		}

		scriptCount := int(r.G1())
		for i := 0; i < scriptCount; i++ {
			opcodeCount := int(r.G2())
			for j := 0; j < opcodeCount; j++ {
				_ = r.G2()
			}
		}

		if com.Type == TypeLayer {
			_ = r.G2() // scrollHeight
			_ = r.G1() // hide
			childCount := int(r.G2())
			com.Children = make([]int, childCount)
			for i := 0; i < childCount; i++ {
				com.Children[i] = int(r.G2())
				_ = r.G2b() // childX (signed)
				_ = r.G2b() // childY (signed)
			}
		}

		if com.Type == TypeUnused {
			r.Skip(3)
		}

		if com.Type == TypeInv {
			r.G1() // objSwap — not used here, read to stay aligned
			com.ObjOps = r.G1() == 1
			com.ObjUse = r.G1() == 1
			r.G1() // objReplace

			_ = r.G1() // marginX
			_ = r.G1() // marginY

			for i := 0; i < 20; i++ {
				if r.G1() == 1 {
					_ = r.G2b()   // invBackgroundX
					_ = r.G2b()   // invBackgroundY
					_ = r.GJStr() // graphic name — media resolution not needed here
				}
			}

			for i := 0; i < 5; i++ {
				com.Iop[i] = r.GJStr()
			}
		}

		if com.Type == TypeRect {
			_ = r.G1() // fill
		}

		if com.Type == TypeText || com.Type == TypeUnused {
			_ = r.G1() // centre
			_ = r.G1() // font
			_ = r.G1() // shadow
		}

		if com.Type == TypeText {
			com.Text = r.GJStr()
			_ = r.GJStr() // text2
		}

		if com.Type == TypeUnused || com.Type == TypeRect || com.Type == TypeText {
			_ = r.G4() // colour
		}

		if com.Type == TypeRect || com.Type == TypeText {
			_ = r.G4() // colour2
			_ = r.G4() // colourOver
			_ = r.G4() // colour2Over
		}

		if com.Type == TypeGraphic {
			_ = r.GJStr() // graphic
			_ = r.GJStr() // activeGraphic
		}

		if com.Type == TypeModel {
			if r.G1() != 0 {
				_ = r.G1()
			}
			if r.G1() != 0 {
				_ = r.G1()
			}
			if r.G1() != 0 { // modelAnim
				_ = r.G1()
			}
			if r.G1() != 0 { // modelAnim2
				_ = r.G1()
			}
			_ = r.G2() // modelZoom
			_ = r.G2() // modelXAn
			_ = r.G2() // modelYAn
		}

		if com.Type == TypeInvText {
			_ = r.G1()  // centre
			_ = r.G1()  // font
			_ = r.G1()  // shadow
			_ = r.G4()  // colour
			_ = r.G2b() // marginX (signed here, unlike TYPE_INV's unsigned byte — see IfType.ts)
			_ = r.G2b() // marginY

			com.ObjOps = r.G1() == 1

			for i := 0; i < 5; i++ {
				com.Iop[i] = r.GJStr()
			}
		}

		if com.ButtonType == ButtonTarget || com.Type == TypeInv {
			_ = r.GJStr() // targetVerb
			_ = r.GJStr() // targetBase
			_ = r.G2()    // targetMask
		}

		if com.ButtonType == ButtonOK || com.ButtonType == ButtonToggle || com.ButtonType == ButtonSelect || com.ButtonType == ButtonContinue {
			com.ButtonText = r.GJStr()
		}

		_ = width
		_ = height
	}

	return out
}

// FindInvComponentIn ports findInvComponentIn: breadth-first from rootComID through
// Children, returning the first TYPE_INV component satisfying accept, or -1.
func FindInvComponentIn(components Components, rootComID int, accept func(*Component) bool) int {
	queue := []int{rootComID}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		com, ok := components[id]
		if !ok {
			continue
		}
		if com.Type == TypeInv && accept(com) {
			return com.ID
		}
		queue = append(queue, com.Children...)
	}
	return -1
}
