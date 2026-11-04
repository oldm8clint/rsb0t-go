package client

import "testing"

// A panicking packet callback is recovered and logged, not allowed to take down the process.
func TestOnPacketSafelyRecoversAPanickingCallback(t *testing.T) {
	var logged string
	s := &Session{}
	s.cfg.Log = func(msg string) { logged = msg }
	s.onPacketSafely(func(Packet) { _ = []byte{}[1] }, Packet{Opcode: 7})
	if logged == "" {
		t.Fatal("the panic was not logged")
	}
}
