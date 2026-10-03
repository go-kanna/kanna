package di_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-kanna/kanna/internal/exit"
	"github.com/go-kanna/kanna/internal/gen/di"
)

const staleGoMod = "module example.com/app\n\ngo 1.25\n"

// regenerate runs the CLI over a module, rewrites app.go, and runs it again,
// returning the second run's exit code and stderr.
func regenerate(t *testing.T, before, after string, extra map[string]string) (string, int, string) {
	t.Helper()

	files := map[string]string{"go.mod": staleGoMod, "app.go": before}
	dir := writeModule(t, files)

	var out, errOut bytes.Buffer
	c := di.CLI{Out: &out, Err: &errOut, Dir: dir}
	if code := c.Run([]string{"./..."}); code != exit.OK {
		t.Fatalf("first run: exit code = %d, want %d\nstderr: %s", code, exit.OK, errOut.String())
	}

	extra["app.go"] = after
	for name, content := range extra {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	errOut.Reset()
	code := c.Run([]string{"./..."})
	return dir, code, errOut.String()
}

const staleBefore = `package app

type DB struct{}

func NewDB() *DB { return nil }

type Container struct {
	DB *DB ` + "`di:\"\"`" + `
}
`

// The old constructor still has the right signature but its body names a
// provider that is gone. A hand-written caller keeps compiling against the stub.
func TestCLI_RegeneratesOverAStaleBody(t *testing.T) {
	t.Parallel()

	after := `package app

type User struct{}

func NewUser() *User { return nil }

type Container struct {
	User *User ` + "`di:\"\"`" + `
}
`
	caller := "package app\n\nfunc run() *Container { return NewContainer() }\n"

	dir, code, stderr := regenerate(t, staleBefore, after, map[string]string{"run.go": caller})
	if code != exit.OK {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, exit.OK, stderr)
	}
	if got := readFile(t, filepath.Join(dir, "di_gen.go")); !strings.Contains(got, "NewUser()") {
		t.Errorf("di_gen.go was not regenerated:\n%s", got)
	}
}

const staleArgAfter = `package app

type DB struct{}

func NewDB() *DB { return nil }

type Container struct {
	DB *DB ` + "`di:\"\"`" + `
}
`

const staleArgBefore = `package app

type Config struct{}

type DB struct{}

func NewDB(*Config) *DB { return nil }

type Container struct {
	_  *Config ` + "`di:\"arg\"`" + `
	DB *DB     ` + "`di:\"\"`" + `
}
`

// The old constructor's signature names a type that is gone, so only dropping
// the whole file lets the package type-check.
func TestCLI_RegeneratesOverAStaleSignature(t *testing.T) {
	t.Parallel()

	dir, code, stderr := regenerate(t, staleArgBefore, staleArgAfter, map[string]string{})
	if code != exit.OK {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, exit.OK, stderr)
	}
	if got := readFile(t, filepath.Join(dir, "di_gen.go")); !strings.Contains(got, "func NewContainer() *Container {") {
		t.Errorf("di_gen.go was not regenerated:\n%s", got)
	}
}

// A hand-written caller of a dropped constructor cannot be helped; the run fails
// on the caller and says why the constructor disappeared.
func TestCLI_StaleSignatureWithACallerExplainsItself(t *testing.T) {
	t.Parallel()

	caller := "package app\n\nfunc run() *Container { return NewContainer(nil) }\n"

	dir, code, stderr := regenerate(t, staleArgBefore, staleArgAfter, map[string]string{"run.go": caller})
	if code != exit.Error {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, exit.Error, stderr)
	}
	if !strings.Contains(stderr, "run.go") {
		t.Errorf("stderr does not report the caller: %s", stderr)
	}
	if !strings.Contains(stderr, "di_gen.go is stale and was set aside") {
		t.Errorf("stderr lacks the note on the stale file: %s", stderr)
	}
	// The file on disk is left as it was.
	if got := readFile(t, filepath.Join(dir, "di_gen.go")); !strings.Contains(got, "config *Config") {
		t.Errorf("di_gen.go changed despite the failure:\n%s", got)
	}
}
