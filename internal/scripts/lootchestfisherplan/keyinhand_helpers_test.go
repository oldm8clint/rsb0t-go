package lootchestfisherplan

import (
	"os"
	"strings"
	"testing"
)

// contentRoot is a Content checkout named by RSB0T_CONTENT; the test skips without one.
func contentRoot(t *testing.T) string {
	t.Helper()
	root := os.Getenv("RSB0T_CONTENT")
	if root == "" {
		t.Skip("set RSB0T_CONTENT to a Content checkout to check the chest script's order against it")
	}
	return strings.TrimRight(root, "/\\")
}

func readFileIfAny(path string) ([]byte, error) { return os.ReadFile(path) }

func indexOf(s, sub string) int { return strings.Index(s, sub) }
