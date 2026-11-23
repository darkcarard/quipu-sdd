package cli

import (
	"os"
	"testing"
)

// TestMain keeps every test in this package from opening a real editor:
// `sdd review` prints the diff instead.
func TestMain(m *testing.M) {
	os.Setenv("SDD_REVIEW_EDITOR", "none")
	os.Exit(m.Run())
}
