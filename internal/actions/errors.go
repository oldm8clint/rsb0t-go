package actions

import "errors"

var (
	errBadOption     = errors.New("actions: inv button option must be 1..5")
	errNoCountDialog = errors.New("actions: no count dialog is open")
	// ErrNotOnScreen is returned by Trade actions when the required trade screen isn't open.
	ErrNotOnScreen = errors.New("actions: required trade screen is not open")
	// ErrItemNotFound is returned when no slot in the relevant inventory holds the item.
	ErrItemNotFound = errors.New("actions: item not found in inventory")
	errNoWaypoints  = errors.New("actions: walk needs at least one waypoint")
)
