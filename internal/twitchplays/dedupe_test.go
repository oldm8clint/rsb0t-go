package twitchplays

import "testing"

func TestDedupeOnlyAcceptsMessageOnce(t *testing.T) {
	d := NewDedupe()
	if !d.First("m1") {
		t.Fatal("first message rejected")
	}
	if d.First("m1") {
		t.Fatal("duplicate accepted")
	}
	if d.First("") {
		t.Fatal("empty ID accepted")
	}
	d.Reset()
	if !d.First("m1") {
		t.Fatal("message not accepted after reset")
	}
}
