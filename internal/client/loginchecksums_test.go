package client

import "testing"

// Given checksums, a login must not touch HTTP at all: an HTTPBase that cannot be reached
// proves no fetch was tried.
func TestLoginChecksumsUsesGivenTableWithoutFetching(t *testing.T) {
	want := [9]int32{1, -2, 3, 4, 5, 6, 7, 8, 9}
	got, err := loginChecksums(Config{HTTPBase: "http://127.0.0.1:1/", Checksums: &want})
	if err != nil {
		t.Fatalf("loginChecksums: %v", err)
	}
	if got != want {
		t.Fatalf("loginChecksums = %v, want %v", got, want)
	}
}

func TestLoginChecksumsFetchesWhenNotGiven(t *testing.T) {
	if _, err := loginChecksums(Config{HTTPBase: "http://127.0.0.1:1/"}); err == nil {
		t.Fatal("with no table and an unreachable HTTPBase, loginChecksums should report the failed fetch")
	}
}
