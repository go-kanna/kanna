package plan_test

import (
	"os"
	"testing"
)

// TestMain loads every module the tests write outside any workspace, which a
// GOWORK of the environment would otherwise leave them outside of.
func TestMain(m *testing.M) {
	// Before any test runs: t.Setenv would rule out t.Parallel.
	if err := os.Setenv("GOWORK", "off"); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}
