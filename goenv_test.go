package main

import (
	"go/build"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
)

func TestGoContext(t *testing.T) {
	t.Setenv("GOOS", "plan9")
	t.Setenv("GOARCH", "arm")
	t.Setenv("CGO_ENABLED", "0")
	t.Setenv("GOFLAGS", "'-tags=foo bar'")

	got, err := goContext(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got.GOOS != "plan9" || got.GOARCH != "arm" || got.CgoEnabled ||
		!slices.Equal(got.BuildTags, []string{"foo", "bar"}) {
		t.Errorf("goContext() = %+v", got)
	}
	if got.GOROOT == "" || got.Compiler != runtime.Compiler || len(got.ToolTags) == 0 ||
		!slices.Equal(got.ReleaseTags, build.Default.ReleaseTags) {
		t.Errorf("goContext() = %+v", got)
	}
}

func TestGen_BadGOFLAGS(t *testing.T) {
	t.Setenv("GOFLAGS", "-tags")

	got := kanna(t, t.TempDir(), "gen")

	if got.code != 1 {
		t.Fatalf("%+v", got)
	}
	contains(t, got.stderr, "read the go environment", "-tags")
}

// savedGoEnv writes settings where GOENV can name them, as go env -w saves them
// but away from what the user saved, and clears what would override them.
func savedGoEnv(t *testing.T, settings string) string {
	t.Helper()

	for _, name := range []string{"GOOS", "GOARCH", "CGO_ENABLED"} {
		t.Setenv(name, "")
	}
	file := filepath.Join(t.TempDir(), "goenv")
	if err := os.WriteFile(file, []byte(settings), 0o600); err != nil {
		t.Fatal(err)
	}
	return file
}

func TestGen_SavedBuildEnvironment(t *testing.T) {
	t.Setenv("GOENV", savedGoEnv(t, "CGO_ENABLED=0\n"))

	dir := project(t, map[string]string{
		"a/a.go": "//go:build !cgo\n\npackage a\n\n//go:generate go tool kanna-fake -out a_gen.go\n",
	})

	got := kanna(t, dir, "gen", "--plan")
	if got.code != 0 {
		t.Fatalf("%+v", got)
	}
	contains(t, got.stdout, "1. a/a.go:5")
}

// A saved GOOS selects the files, and reaches go tool, which builds what it runs
// for that GOOS. That is no success with nothing to do, as a run of what only
// this machine's files say would have it.
func TestGen_SavedTargetOfAnotherMachine(t *testing.T) {
	goos := "linux"
	if runtime.GOOS == goos {
		goos = "darwin"
	}
	t.Setenv("GOENV", savedGoEnv(t, "GOOS="+goos+"\n"))

	file := "a/a_" + goos + ".go"
	dir := project(t, map[string]string{
		"a/doc.go": "package a\n",
		file:       "package a\n\n//go:generate go tool kanna-fake -out a_gen.go\n",
	})

	got := kanna(t, dir, "gen", "--check")
	if got.code != 1 {
		t.Fatalf("%+v", got)
	}
	contains(t, got.stderr, file+":3", "kanna-fake cannot run on this machine", "for "+goos+"/"+runtime.GOARCH)
}

func TestGen_GOFLAGSBuildTags(t *testing.T) {
	t.Setenv("GOFLAGS", "-tags=kannatest")

	dir := project(t, map[string]string{
		"a/a.go":      "package a\n\n//go:generate go tool kanna-fake -out a_gen.go\n",
		"a/tagged.go": "//go:build kannatest\n\npackage a\n\nimport _ \"example.com/m/b\"\n",
		"b/b.go":      "//go:build kannatest\n\npackage b\n\n//go:generate go tool kanna-fake -out b_gen.go\n",
	})

	got := kanna(t, dir, "gen", "--plan", "./a")
	if got.code != 0 {
		t.Fatalf("%+v", got)
	}
	contains(t, got.stdout, "1. b/b.go:5", "2. a/a.go:3", "reads b, which kanna-fake writes into")

	got = kanna(t, dir, "gen", "--check", "./b")
	if got.code != 1 {
		t.Fatalf("%+v", got)
	}
	contains(t, got.stdout, "b/b.go:5", "out of date")
}
