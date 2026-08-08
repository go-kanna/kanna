package version_test

import (
	"testing"

	"github.com/go-kanna/kanna/internal/version"
)

func TestString_IsNeverEmpty(t *testing.T) {
	t.Parallel()

	// Under `go test` the go command stamps the module as "(devel)", not a real
	// version. What matters is that --version never prints nothing.
	if got := version.String(); got == "" {
		t.Error("String() = \"\", want a non-empty version")
	}
}
