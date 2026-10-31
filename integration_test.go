package main

import (
	"fmt"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/go-kanna/kanna/internal/module"
)

// TestProject checks kanna gen against a real project, at the path KANNA_PROJECT
// names, whose generators answer -deps: what is committed holds, generating
// twice changes nothing, and generating once brings back all that kanna
// generators wrote. Checking repeatedly would not tell the last: an order that
// needs two runs passes on a tree that has had them.
func TestProject(t *testing.T) {
	project := os.Getenv("KANNA_PROJECT")
	if project == "" {
		t.Skip("KANNA_PROJECT names no project")
	}
	// What TestMain turns off for the modules of the other tests: a project is
	// run as it is, with the go.work it may hold.
	t.Setenv("GOWORK", "")

	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	copyTree(t, project, dir)

	// What is committed holds.
	if got := kanna(t, dir, "gen", "--check"); got.code != 0 {
		t.Fatalf("--check: %+v", got)
	}

	// Generating again changes nothing.
	before := tree(t, dir)
	if got := kanna(t, dir, "gen"); got.code != 0 {
		t.Fatalf("first run: %+v", got)
	}
	if got := kanna(t, dir, "gen"); got.code != 0 || !strings.Contains(got.stdout, " 0 files changed\n") {
		t.Fatalf("second run: %+v", got)
	}
	sameTree(t, tree(t, dir), before)

	// One run brings back what is gone.
	removeGenerated(t, dir)
	if got := kanna(t, dir, "gen"); got.code != 0 {
		t.Fatalf("run from nothing: %+v", got)
	}
	sameTree(t, tree(t, dir), before)
	for _, args := range [][]string{{"build", "./..."}, {"vet", "./..."}} {
		cmd := exec.CommandContext(t.Context(), "go", args...) //nolint:gosec // the two above
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Errorf("go %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
}

// removeGenerated removes what kanna generators wrote in the module at dir.
func removeGenerated(t *testing.T, dir string) {
	t.Helper()

	m, err := module.Find(dir)
	if err != nil {
		t.Fatal(err)
	}
	removed := 0
	for _, p := range m.Packages {
		for _, file := range slices.Sorted(maps.Keys(p.Generated)) {
			if err := os.Remove(file); err != nil {
				t.Fatal(err)
			}
			removed++
		}
	}
	if removed == 0 {
		t.Fatal("nothing generated to remove")
	}
}

// copyTree copies the tree at src to dst, leaving out what git and tools keep.
func copyTree(t *testing.T, src, dst string) {
	t.Helper()

	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error { //nolint:gosec // the test's own copy
		if err != nil {
			return err
		}
		switch {
		case d.IsDir() && (d.Name() == ".git" || d.Name() == "tmp" || d.Name() == "bin"):
			return filepath.SkipDir
		case !d.IsDir() && !d.Type().IsRegular():
			return nil
		}
		return copyEntry(src, dst, path, d)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// copyEntry copies path, a file or directory under src, to its place under dst.
func copyEntry(src, dst, path string, d fs.DirEntry) error {
	rel, err := filepath.Rel(src, path)
	if err != nil {
		return fmt.Errorf("copy %s: %w", path, err)
	}
	to := filepath.Join(dst, rel)
	if d.IsDir() {
		return os.MkdirAll(to, 0o750) //nolint:wrapcheck // a *fs.PathError, which names the directory
	}
	data, err := os.ReadFile(path) //nolint:gosec // a file of the project
	if err != nil {
		return fmt.Errorf("copy %s: %w", path, err)
	}
	return os.WriteFile(to, data, 0o600) //nolint:wrapcheck,gosec // a *fs.PathError, which names the file
}
