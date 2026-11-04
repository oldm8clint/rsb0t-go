package observation

// State is the compact, versioned observation presented to an ML advisor.
// It contains derived values rather than references to mutable game state.
type State struct {
	SchemaVersion  uint16
	Tick           uint64
	X, Y           int
	InventoryUsed  uint8
	InventorySize  uint8
	Health         uint16
	XPDelta        int32
	DistanceToFish uint16
	DistanceToBank uint16
	NearbyFish     uint8
	NearbyBank     uint8
	LastAction     string
	LastActionOK   bool
}
