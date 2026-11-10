package main

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// fakeVet is a generator with a vet, built as a tool named what replaces NAME.
// Its vet logs what it is given to vet.log in the directory it runs in, and
// fails printing what NAME.out holds there, when there is one.
const fakeVet = `package main

import (
	"fmt"
	"os"
	"strings"
)

func main() {
	args := os.Args[1:]
	switch strings.Join(args, " ") {
	case "-version", "vet -version":
		fmt.Println("NAME v0.0.1")
		return
	}
	if len(args) == 0 || args[0] != "vet" {
		os.Exit(2)
	}
	f, err := os.OpenFile("vet.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Fprintln(f, strings.Join(append([]string{"NAME"}, args[1:]...), " "))
	_ = f.Close()
	if out, err := os.ReadFile("NAME.out"); err == nil {
		fmt.Fprint(os.Stderr, string(out))
		os.Exit(1)
	}
}
`

// noVet is a generator from before its vet, which reads vet as a package.
const noVet = `package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "-version" {
		fmt.Println("NAME v0.0.1")
		return
	}
	fmt.Fprintln(os.Stderr, "-: package vet is not in std")
	os.Exit(1)
}
`

// vetProject writes a module with files, kanna-fake as a tool, and a tool of
// each source of tools, by name, and returns its directory.
func vetProject(t *testing.T, files, tools map[string]string) string {
	t.Helper()

	var gomod strings.Builder
	gomod.WriteString("module example.com/m\n\ngo 1.26.0\n\ntool example.com/m/cmd/kanna-fake\n")
	for _, name := range slices.Sorted(maps.Keys(tools)) {
		files["cmd/"+name+"/main.go"] = strings.ReplaceAll(tools[name], "NAME", name)
		gomod.WriteString("tool example.com/m/cmd/" + name + "\n")
	}
	dir := project(t, files)
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

// vetLines is a module running kanna-i18n in a, and kanna-value and kanna-fake,
// which has no vet, in b.
func vetLines() map[string]string {
	return map[string]string{
		"a/a.go": "package a\n\n//go:generate go tool kanna-i18n\n",
		"b/b.go": "package b\n\n//go:generate go tool kanna-value\n//go:generate go tool kanna-fake -out b_gen.go\n",
	}
}

// logged checks what the vets that ran in dir were given, in order.
func logged(t *testing.T, dir string, want ...string) {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(dir, "vet.log")) //nolint:gosec // a file of the test's module
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n"); !slices.Equal(got, want) {
		t.Errorf("the vets ran with %q, want %q", got, want)
	}
}

func TestVet(t *testing.T) {
	t.Parallel()

	tools := map[string]string{"kanna-value": fakeVet, "kanna-i18n": fakeVet, "kanna-orm": fakeVet}
	dir := vetProject(t, vetLines(), tools)

	got := kanna(t, dir, "vet")

	if got != (outcome{}) {
		t.Errorf("%+v", got)
	}
	logged(t, dir, "kanna-value ./...", "kanna-i18n ./...")
}

func TestVet_ReportsBelowEachVet(t *testing.T) {
	t.Parallel()

	dir := vetProject(t, vetLines(), map[string]string{"kanna-value": fakeVet, "kanna-i18n": fakeVet})
	report := `./b/b.go:3:1: error: "clsoed" is none of the values of Status` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "kanna-value.out"), []byte(report), 0o600); err != nil {
		t.Fatal(err)
	}

	got := kanna(t, dir, "vet")

	if got.code != 1 || got.stdout != "" {
		t.Fatalf("%+v", got)
	}
	if want := "# kanna-value\n" + report; got.stderr != want {
		t.Errorf("stderr:\n%s\nwant:\n%s", got.stderr, want)
	}
	logged(t, dir, "kanna-value ./...", "kanna-i18n ./...")
}

func TestVet_TagsAndPatterns(t *testing.T) {
	t.Parallel()

	dir := vetProject(t, vetLines(), map[string]string{"kanna-value": fakeVet, "kanna-i18n": fakeVet})

	got := kanna(t, dir, "vet", "-tags", "x,y", "./a", "./b")

	if got.code != 0 {
		t.Fatalf("%+v", got)
	}
	logged(t, dir, "kanna-value -tags x,y ./a ./b", "kanna-i18n -tags x,y ./a ./b")
}

// The tags choose the files whose lines tell what vets run, as they choose the
// files the vets read.
func TestVet_TagsChooseTheLines(t *testing.T) {
	t.Parallel()

	files := map[string]string{
		"a/a.go":     "package a\n",
		"a/extra.go": "//go:build extra\n\npackage a\n\n//go:generate go tool kanna-value\n",
	}
	dir := vetProject(t, files, map[string]string{"kanna-value": fakeVet})

	got := kanna(t, dir, "vet", "-tags", "extra")

	if got != (outcome{}) {
		t.Fatalf("%+v", got)
	}
	logged(t, dir, "kanna-value -tags extra ./...")
}

// What vets run is told by the whole module, wherever in it kanna vet runs, and
// they run there.
func TestVet_FromBelowTheRoot(t *testing.T) {
	t.Parallel()

	dir := vetProject(t, vetLines(), map[string]string{"kanna-value": fakeVet, "kanna-i18n": fakeVet})

	got := kanna(t, filepath.Join(dir, "b"), "vet", ".")

	if got.code != 0 {
		t.Fatalf("%+v", got)
	}
	logged(t, filepath.Join(dir, "b"), "kanna-value .", "kanna-i18n .")
}

func TestVet_NoGeneratorWithAVet(t *testing.T) {
	t.Parallel()

	dir := project(t, map[string]string{"a/a.go": "package a\n\n//go:generate go tool kanna-fake -out a_gen.go\n"})

	got := kanna(t, dir, "vet")

	if got.code != 0 {
		t.Fatalf("%+v", got)
	}
	contains(t, got.stderr, "warning: none of the generators the module runs has a vet",
		"those with one are kanna-value, kanna-orm, kanna-i18n")
}

// A generator that cannot vet keeps every vet from running, as a problem of
// kanna gen keeps every task from running.
func TestVet_AGeneratorWithoutAVet(t *testing.T) {
	t.Parallel()

	files := vetLines()
	files["a/a.go"] = "package a\n\n//go:generate go tool kanna-orm\n"
	dir := vetProject(t, files, map[string]string{"kanna-value": fakeVet, "kanna-orm": noVet})

	got := kanna(t, dir, "vet")

	if got.code != 1 {
		t.Fatalf("%+v", got)
	}
	contains(t, got.stderr, "a/a.go:3: error: kanna-orm v0.0.1 has no vet; "+
		"update it: go get -tool github.com/go-kanna/orm/cmd/kanna-orm@latest")
	if exists(filepath.Join(dir, "vet.log")) {
		t.Error("a vet ran")
	}
}

func TestVet_NotATool(t *testing.T) {
	t.Parallel()

	dir := vetProject(t, vetLines(), map[string]string{"kanna-value": fakeVet})

	got := kanna(t, dir, "vet")

	if got.code != 1 {
		t.Fatalf("%+v", got)
	}
	contains(t, got.stderr, "a/a.go:3: error: kanna-i18n is not a tool of this module; "+
		"add it: go get -tool github.com/go-kanna/i18n/cmd/kanna-i18n@latest")
}

// A tool that does not build is no tool with no vet: what the go command says
// is what tells.
func TestVet_AToolThatDoesNotBuild(t *testing.T) {
	t.Parallel()

	broken := "package main\n\nfunc main() { undefined() }\n"
	dir := vetProject(t, vetLines(), map[string]string{"kanna-value": broken, "kanna-i18n": fakeVet})

	got := kanna(t, dir, "vet")

	if got.code != 1 {
		t.Fatalf("%+v", got)
	}
	contains(t, got.stderr, "b/b.go:3: error: go tool kanna-value vet -version: exit status 1:", "undefined")
}

func TestVet_AGeneratorThatDoesNotBuild(t *testing.T) {
	t.Parallel()

	dir := releasedTool(t, map[string]string{"b/b.go": "package b\n\n//go:generate go tool kanna-value\n"})

	got := kanna(t, dir, "vet")

	if got.code != 1 {
		t.Fatalf("%+v", got)
	}
	contains(t, got.stderr, "b/b.go:3: error: kanna-value v0.0.7 does not build with "+
		"github.com/go-kanna/internal v0.0.29, which this module selects; "+
		"update it: go get -tool github.com/go-kanna/value/cmd/kanna-value@latest, or update every tool: go get tool")
}

func TestVet_OutsideAModule(t *testing.T) {
	t.Parallel()

	got := kanna(t, t.TempDir(), "vet")

	if got.code != 1 {
		t.Errorf("%+v", got)
	}
	contains(t, got.stderr, "no go.mod")
}
