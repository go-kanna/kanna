package di

import (
	"bytes"
	"testing"
)

// RenderForTest collects imports for p into the supplied tracker (whose reserved
// aliases the caller has already set up) and renders just the container
// constructor. It exists for tests that need to inject reserved import names
// without round-tripping through Emit.
func RenderForTest(t *testing.T, im *Imports, p Plan) string {
	t.Helper()

	collectImports(im, p)

	var buf bytes.Buffer
	if err := writeContainer(&buf, im, p); err != nil {
		t.Fatalf("writeContainer: %v", err)
	}
	return buf.String()
}
