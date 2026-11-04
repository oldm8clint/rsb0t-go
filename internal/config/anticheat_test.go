package config

import "testing"

func TestAntiCheatPacketsDefaultOnAndJagChecksums(t *testing.T) {
	g := defaultGlobal()
	if !g.Server.AntiCheatPackets {
		t.Fatal("antiCheatPackets must default to true, the behaviour before the setting existed")
	}

	var p ServerProfile
	if p.JagChecksums() != nil {
		t.Fatal("a profile with no crcTable should fetch its checksums (nil)")
	}
	p.CRC = []uint32{1, 2, 3, 4, 5, 6, 7, 8, 0xFFFFFFFF}
	got := p.JagChecksums()
	if got == nil {
		t.Fatal("nine checksums should be returned")
	}
	if got[0] != 1 || got[8] != -1 {
		t.Fatalf("JagChecksums = %v, want 1..8 then -1 (the TS client's Int32Array view)", *got)
	}
}
