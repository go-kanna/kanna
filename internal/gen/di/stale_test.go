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

// Dropping the whole file leaves a hand-written caller referring to a
// constructor that does not exist. That is a call to a constructor this run
// writes, so it is set aside and the file is regenerated; the compiler then
// tells the caller about the new signature.
func TestCLI_RegeneratesOverAStaleSignatureDespiteACaller(t *testing.T) {
	t.Parallel()

	caller := "package app\n\nfunc run() *Container { return NewContainer(nil) }\n"

	dir, code, stderr := regenerate(t, staleArgBefore, staleArgAfter, map[string]string{"run.go": caller})
	if code != exit.OK {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, exit.OK, stderr)
	}
	if got := readFile(t, filepath.Join(dir, "di_gen.go")); !strings.Contains(got, "func NewContainer() *Container {") {
		t.Errorf("di_gen.go was not regenerated:\n%s", got)
	}
}

// Setting a stale file aside only helps with the errors it caused. Anything
// else still fails the run, with a note on why the file was set aside.
func TestCLI_StaleFileNoteWhenOtherErrorsRemain(t *testing.T) {
	t.Parallel()

	broken := "package app\n\nfunc run() *Container { return NewContainer(nothingHere) }\n"

	dir, code, stderr := regenerate(t, staleArgBefore, staleArgAfter, map[string]string{"run.go": broken})
	if code != exit.Error {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, exit.Error, stderr)
	}
	if !strings.Contains(stderr, "undefined: nothingHere") {
		t.Errorf("stderr does not report the real error: %s", stderr)
	}
	if !strings.Contains(stderr, "di_gen.go is stale and was set aside") {
		t.Errorf("stderr lacks the note on the stale file: %s", stderr)
	}
	// The file on disk is left as it was.
	if got := readFile(t, filepath.Join(dir, "di_gen.go")); !strings.Contains(got, "config *Config") {
		t.Errorf("di_gen.go changed despite the failure:\n%s", got)
	}
}

// The constructor of a test container lives in di_gen_test.go, which goes stale
// like di_gen.go does and is set aside the same way.
func TestCLI_RegeneratesOverAStaleTestFile(t *testing.T) {
	t.Parallel()

	envTest := `package app

//kanna:container name=newEnv
type env struct {
	db *DB ` + "`di:\"\"`" + `
}
`
	dir := writeModule(t, map[string]string{
		"go.mod":      staleGoMod,
		"app.go":      staleBefore,
		"env_test.go": envTest,
	})

	var out, errOut bytes.Buffer
	c := di.CLI{Out: &out, Err: &errOut, Dir: dir}
	if code := c.Run([]string{"./..."}); code != exit.OK {
		t.Fatalf("first run: exit code = %d, want %d\nstderr: %s", code, exit.OK, errOut.String())
	}

	// Renaming the provider leaves both generated bodies calling a function
	// that no longer exists.
	after := strings.Replace(staleBefore, "func NewDB()", "func OpenDB()", 1)
	if err := os.WriteFile(filepath.Join(dir, "app.go"), []byte(after), 0o600); err != nil {
		t.Fatal(err)
	}

	errOut.Reset()
	if code := c.Run([]string{"./..."}); code != exit.OK {
		t.Fatalf("second run: exit code = %d, want %d\nstderr: %s", code, exit.OK, errOut.String())
	}
	if generated := readFile(t, filepath.Join(dir, "di_gen_test.go")); !strings.Contains(generated, "OpenDB()") {
		t.Errorf("di_gen_test.go was not regenerated:\n%s", generated)
	}
}
