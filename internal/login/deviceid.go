package login

import (
	"hash/fnv"
	"strings"
)

// Device id modes for a fleet. DeviceIDFixed sends DefaultDeviceID from every account, as the
// web client does; DeviceIDPerAccount gives each account its own stable id, as if each one
// played from its own machine.
const (
	DeviceIDFixed      = "fixed"
	DeviceIDPerAccount = "per-account"
)

// AccountDeviceID returns username's stable per-account device id: the same name (any case)
// always gives the same id across restarts and reconnects, as a real machine would. It is
// never 0 (which Options reads as "use the default") and never DefaultDeviceID.
func AccountDeviceID(username string) int32 {
	h := fnv.New32a()
	h.Write([]byte("rsb0t-device:"))
	h.Write([]byte(strings.ToLower(strings.TrimSpace(username))))
	id := int32(h.Sum32())
	if id == 0 || id == DefaultDeviceID {
		id ^= 0x5bd1e995
	}
	return id
}

// DeviceIDFor returns the id an account sends under mode: 0 (the default) for "" or
// DeviceIDFixed, AccountDeviceID for DeviceIDPerAccount.
func DeviceIDFor(mode, username string) int32 {
	if mode == DeviceIDPerAccount {
		return AccountDeviceID(username)
	}
	return 0
}

// ValidDeviceIDMode reports whether mode is "", DeviceIDFixed or DeviceIDPerAccount.
func ValidDeviceIDMode(mode string) bool {
	return mode == "" || mode == DeviceIDFixed || mode == DeviceIDPerAccount
}
