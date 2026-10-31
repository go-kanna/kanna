package plan_test

import (
	"os/exec"
	"strings"
	"testing"
)

// goroot returns the GOROOT of the go command.
func goroot(t *testing.T) string {
	t.Helper()

	out, err := exec.CommandContext(t.Context(), "go", "env", "GOROOT").Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}
