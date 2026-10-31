package generate_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/go-kanna/kanna/internal/generate"
)

func TestScan(t *testing.T) {
	t.Parallel()

	src := "package p\n\n//go:generate go tool kanna-value\n" +
		"//go:generate\tgo tool kanna-validate\r\n" +
		"// go:generate spaced\n" +
		"  //go:generate indented\n" +
		"//go:generatefoo\n" +
		"//go:generate\n" +
		"var s = `\n//go:generate in a raw string\n`\n" +
		"//go:generate at the end"

	got := generate.Scan("/m/p/p.go", []byte(src), "p")

	want := []generate.Directive{
		{File: "/m/p/p.go", Line: 3, Package: "p", Text: "go tool kanna-value"},
		{File: "/m/p/p.go", Line: 4, Package: "p", Text: "go tool kanna-validate"},
		{File: "/m/p/p.go", Line: 10, Package: "p", Text: "in a raw string"},
		{File: "/m/p/p.go", Line: 12, Package: "p", Text: "at the end"},
	}
	if !slices.Equal(got, want) {
		t.Errorf("Scan() = %+v, want %+v", got, want)
	}
}

func TestDirective_Words(t *testing.T) {
	t.Parallel()

	d := generate.Directive{File: "/m/p/f.go", Line: 7, Package: "p"}
	env := d.Env("/goroot")

	tests := []struct {
		text string
		want []string
	}{
		{text: "go tool kanna-value", want: []string{"go", "tool", "kanna-value"}},
		{text: "  go \t tool  ", want: []string{"go", "tool"}},
		{text: `go tool "kanna-orm" -destination "a b"`, want: []string{"go", "tool", "kanna-orm", "-destination", "a b"}},
		{text: `x "a\tb" "c\"d"`, want: []string{"x", "a\tb", `c"d`}},
		{text: `x a"b c"`, want: []string{"x", `a"b`, `c"`}},
		{text: "x $GOFILE:$GOLINE ${GOPACKAGE}y", want: []string{"x", "f.go:7", "py"}},
		{text: "x $DOLLAR", want: []string{"x", "$"}},
		{text: `x "$GOFILE"`, want: []string{"x", "f.go"}},
		{text: "x $GOROOT/bin", want: []string{"x", "/goroot/bin"}},
		{text: "", want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			t.Parallel()

			line := d
			line.Text = tt.text
			got, err := line.Words(env)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("Words() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDirective_Words_FromTheEnvironment(t *testing.T) {
	t.Setenv("KANNA_TEST_WORD", "from the environment")

	d := generate.Directive{Text: "x $KANNA_TEST_WORD"}
	got, err := d.Words(nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"x", "from the environment"}; !slices.Equal(got, want) {
		t.Errorf("Words() = %q, want %q", got, want)
	}
}

func TestDirective_Words_Errors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		text string
		want string
	}{
		{text: `x "a`, want: "does not end"},
		{text: `x "a\`, want: "does not end"},
		{text: `x "a"b`, want: "expected a space"},
		{text: `x "\q"`, want: "bad quoted string"},
	}
	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			t.Parallel()

			d := generate.Directive{Text: tt.text}
			if _, err := d.Words(nil); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("Words() = %v, want an error saying %q", err, tt.want)
			}
		})
	}
}

func TestDirective_Command(t *testing.T) {
	t.Parallel()

	out, err := exec.CommandContext(t.Context(), "go", "env", "GOROOT").Output()
	if err != nil {
		t.Fatal(err)
	}
	goroot := strings.TrimSpace(string(out))
	dir := t.TempDir()
	d := generate.Directive{File: filepath.Join(dir, "f.go"), Line: 7, Package: "p"}

	cmd := d.Command(t.Context(), goroot, []string{"go", "version"})
	if want := filepath.Join(goroot, "bin", "go"); cmd.Path != want {
		t.Errorf("Path = %q, want %q", cmd.Path, want)
	}
	if cmd.Args[0] != "go" {
		t.Errorf("Args[0] = %q, want go", cmd.Args[0])
	}
	if cmd.Dir != dir {
		t.Errorf("Dir = %q, want %q", cmd.Dir, dir)
	}
	if !slices.Contains(cmd.Env, "GOFILE=f.go") {
		t.Errorf("Env holds no GOFILE: %q", cmd.Env)
	}
}

func TestDirective_Env(t *testing.T) {
	t.Setenv("PATH", "/usr/bin")
	t.Setenv("GOOS", "plan9")
	t.Setenv("GOARCH", "amd64")

	d := generate.Directive{File: "/m/p/f.go", Line: 7, Package: "p_test"}

	want := []string{
		"GOROOT=/goroot",
		"GOARCH=amd64",
		"GOOS=plan9",
		"GOFILE=f.go",
		"GOLINE=7",
		"GOPACKAGE=p_test",
		"DOLLAR=$",
		"PATH=" + filepath.Join("/goroot", "bin") + string(os.PathListSeparator) + "/usr/bin",
		"PWD=/m/p",
	}
	if got := d.Env("/goroot"); !slices.Equal(got, want) {
		t.Errorf("Env() = %q, want %q", got, want)
	}
}
