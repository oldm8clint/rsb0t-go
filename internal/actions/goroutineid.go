package actions

import "runtime"

// GoroutineID is the calling goroutine's id, read from the first line of its stack ("goroutine 123 [running]:").
// Go has no API for it; the input hold (movehold.go) needs "is this click the holder's own" and the holder is a
// goroutine. It costs a stack header per call and is only asked while a hold is in force.
func GoroutineID() uint64 {
	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	const prefix = "goroutine "
	b := buf[:n]
	if len(b) < len(prefix) {
		return 0
	}
	b = b[len(prefix):]
	var id uint64
	for _, c := range b {
		if c < '0' || c > '9' {
			break
		}
		id = id*10 + uint64(c-'0')
	}
	return id
}
