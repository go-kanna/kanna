package plan_test

import (
	"context"
	"fmt"
	"go/build"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/go-kanna/internal/deps"
	"github.com/go-kanna/internal/diag"

	"github.com/go-kanna/kanna/internal/module"
	"github.com/go-kanna/kanna/internal/plan"
)

const modulePath = "example.com/m"

// write writes files under a new directory, as a module, and returns it, with
// the links of the temporary directory followed.
func write(t *testing.T, files map[string]string) string {
	t.Helper()

	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	files["go.mod"] = "module " + modulePath + "\n\ngo 1.26.0\n"
	for name, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// answers holds what tasks answer, by "<dir> <tool>", with paths relative to the
// module and packages relative to its path.
type answers map[string]deps.Report

func (a answers) ask(dir string) plan.Asker {
	return func(_ context.Context, task *plan.Task) (deps.Report, error) {
		rel, err := filepath.Rel(dir, filepath.Dir(task.At[0].File))
		if err != nil {
			return deps.Report{}, fmt.Errorf("task outside the module: %w", err)
		}
		key := filepath.ToSlash(rel) + " " + task.Tool
		if len(task.Args) > 0 {
			key += " " + strings.Join(task.Args, " ")
		}
		r, ok := a[key]
		if !ok {
			return deps.Report{}, fmt.Errorf("nothing answers %q", key)
		}
		r.Packages = slices.Clone(r.Packages)
		for i, p := range r.Packages {
			r.Packages[i] = modulePath + "/" + p
		}
		r.Writes = slices.Clone(r.Writes)
		for i, w := range r.Writes {
			r.Writes[i] = filepath.Join(dir, filepath.FromSlash(w))
		}
		r.Inputs = slices.Clone(r.Inputs)
		for i, in := range r.Inputs {
			r.Inputs[i] = filepath.Join(dir, filepath.FromSlash(in))
		}
		r.Declares = slices.Clone(r.Declares)
		for i, name := range r.Declares {
			r.Declares[i] = modulePath + "/" + name
		}
		return r, nil
	}
}

func makePlan(t *testing.T, dir string, a answers, patterns ...string) *plan.Plan {
	t.Helper()

	m, err := module.Find(dir, build.Default)
	if err != nil {
		t.Fatal(err)
	}
	selection, err := m.Select(dir, patterns)
	if err != nil {
		t.Fatal(err)
	}
	p, err := plan.Make(t.Context(), plan.Config{
		Module: m, Selection: selection, Patterns: patterns, Build: build.Context{GOROOT: "/goroot"}, Ask: a.ask(dir),
	})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// tasks names the tasks of p, in order, by "<file>:<line> <tool>".
func tasks(p *plan.Plan) []string {
	names := make([]string, len(p.Tasks))
	for i, task := range p.Tasks {
		names[i] = fmt.Sprintf("%s:%d %s", p.Module.Rel(task.At[0].File), task.At[0].Line, task.Tool)
	}
	return names
}

func wantOrder(t *testing.T, p *plan.Plan, want ...string) {
	t.Helper()

	if len(p.Problems) > 0 {
		t.Errorf("Problems = %v", p.Problems)
	}
	if got := tasks(p); !slices.Equal(got, want) {
		t.Errorf("tasks = %q, want %q", got, want)
	}
}

func wantProblem(t *testing.T, p *plan.Plan, want string) {
	t.Helper()

	if !slices.ContainsFunc(p.Problems, func(d diag.Diag) bool { return strings.Contains(d.String(), want) }) {
		t.Errorf("Problems = %v, want one saying %q", p.Problems, want)
	}
}

func written(p *plan.Plan) string {
	var b strings.Builder
	p.Write(&b)
	return b.String()
}

func TestMake_ImportsDecideTheOrder(t *testing.T) {
	t.Parallel()

	// z comes last by position, and first by what a imports.
	dir := write(t, map[string]string{
		"a/a.go": "package a\n\nimport _ \"example.com/m/b\"\n\n//go:generate go tool kanna-validate\n",
		"b/b.go": "package b\n\nimport _ \"example.com/m/z\"\n",
		"z/z.go": "package z\n\n//go:generate go tool kanna-value\n",
	})

	p := makePlan(t, dir, answers{
		"a kanna-validate": {Packages: []string{"a"}, Writes: []string{"a/validate_gen.go"}},
		"z kanna-value":    {Packages: []string{"z"}, Writes: []string{"z/value_gen.go"}},
	})

	wantOrder(t, p, "z/z.go:3 kanna-value", "a/a.go:5 kanna-validate")
	want := "1. z/z.go:3\n" +
		"   run:    go tool kanna-value\n" +
		"   writes: z/value_gen.go\n" +
		"\n" +
		"2. a/a.go:5\n" +
		"   run:    go tool kanna-validate\n" +
		"   writes: a/validate_gen.go\n" +
		"   after:  1 — reads z, which kanna-value writes into\n"
	if got := written(p); got != want {
		t.Errorf("Write() =\n%s\nwant\n%s", got, want)
	}
}

func TestMake_WithoutADependencyTheModulesOrderStands(t *testing.T) {
	t.Parallel()

	dir := write(t, map[string]string{
		"b/b.go": "package b\n\n//go:generate go tool kanna-value\n",
		"a/a.go": "package a\n\n//go:generate go tool kanna-value\n//go:generate go tool kanna-validate\n",
	})

	p := makePlan(t, dir, answers{
		"a kanna-value":    {Packages: []string{"a"}},
		"a kanna-validate": {Packages: []string{"a"}},
		"b kanna-value":    {Packages: []string{"b"}},
	})

	wantOrder(t, p, "a/a.go:3 kanna-value", "a/a.go:4 kanna-validate", "b/b.go:3 kanna-value")
}

// Output in another directory than the line's is tied to what reads it.
func TestMake_OutputsElsewhere(t *testing.T) {
	t.Parallel()

	dir := write(t, map[string]string{
		"model/m.go": "package model\n\n//go:generate go tool kanna-fixture\n",
		"repo/r.go":  "package repo\n\n//go:generate go tool kanna-orm -destination ../pg\n",
		"use/u.go":   "package use\n\nimport _ \"example.com/m/model/fixture\"\n\n//go:generate go tool kanna-validate\n",
		"zdi/d.go":   "package zdi\n\n//go:generate go tool kanna-di\n",
	})

	p := makePlan(t, dir, answers{
		"model kanna-fixture": {Packages: []string{"model"}, Writes: []string{"model/fixture/fixture_gen.go"}},
		"repo kanna-orm -destination ../pg": {
			Packages: []string{"repo"},
			Writes:   []string{"pg/orm_gen.go"},
		},
		"use kanna-validate": {Packages: []string{"use"}, Writes: []string{"use/validate_gen.go"}},
		"zdi kanna-di": {
			Packages: []string{"zdi"},
			Refs:     []deps.Ref{{Package: "pg", Via: "with=pg.New"}},
			Writes:   []string{"zdi/di_gen.go"},
		},
	})

	wantOrder(t, p,
		"model/m.go:3 kanna-fixture", "repo/r.go:3 kanna-orm", "use/u.go:5 kanna-validate", "zdi/d.go:3 kanna-di")
	got := written(p)
	for _, want := range []string{
		"after:  1 — reads model/fixture, which kanna-fixture writes into",
		"after:  2 — with=pg.New names pg, which kanna-orm writes into",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Write() =\n%s\nwant it to say %q", got, want)
		}
	}
}

// What a package reads through its tests is no reason to wait for anyone but a
// generator that loads them.
func TestMake_Tests(t *testing.T) {
	t.Parallel()

	dir := write(t, map[string]string{
		"a/a.go":      "package a\n\n//go:generate go tool kanna-di\n//go:generate go tool kanna-validate\n",
		"a/a_test.go": "package a\n\nimport _ \"example.com/m/z\"\n",
		"z/z.go":      "package z\n\n//go:generate go tool kanna-value\n",
	})

	p := makePlan(t, dir, answers{
		"a kanna-di":       {Packages: []string{"a"}, Tests: true},
		"a kanna-validate": {Packages: []string{"a"}},
		"z kanna-value":    {Packages: []string{"z"}, Writes: []string{"z/value_gen.go"}},
	})

	wantOrder(t, p, "a/a.go:4 kanna-validate", "z/z.go:3 kanna-value", "a/a.go:3 kanna-di")
	if got := written(p); !strings.Contains(got, "its tests read z, which kanna-value writes into") {
		t.Errorf("Write() =\n%s", got)
	}
}

func TestMake_SamePackage(t *testing.T) {
	t.Parallel()

	const (
		first  = "package p\n\n//go:generate go tool kanna-di\n//go:generate go tool kanna-value\n"
		marked = "\n//kanna:validate\ntype In struct{}\n\nvar _ = In{}.Validate()\n"
	)
	di := deps.Report{Packages: []string{"p"}, Writes: []string{"p/di_gen.go"}, Declares: []string{"p.NewC"}}
	value := deps.Report{
		Packages: []string{"p"}, SourcesOnly: true, Writes: []string{"p/value_gen.go"}, Declares: []string{"p.NewEmail"},
	}
	validate := deps.Report{
		Packages: []string{"p"}, Writes: []string{"p/validate_gen.go"}, Declares: []string{"p.In.Validate"},
	}

	tests := []struct {
		name  string
		src   string
		a     answers
		order []string
		says  string
	}{
		{
			// kanna-value reads what is written by hand alone, and the other
			// reads what it writes.
			name:  "one reads what the other generates",
			src:   first,
			a:     answers{"p kanna-di": di, "p kanna-value": value},
			order: []string{"p/p.go:4 kanna-value", "p/p.go:3 kanna-di"},
			says:  "after:  1 — reads what kanna-value generates in p",
		},
		{
			name: "code calls what one generates",
			src: "package p\n\n//go:generate go tool kanna-di\n//go:generate go tool kanna-validate\n" +
				marked,
			a:     answers{"p kanna-di": di, "p kanna-validate": validate},
			order: []string{"p/p.go:4 kanna-validate", "p/p.go:3 kanna-di"},
			says:  "after:  1 — code in p calls what kanna-validate generates",
		},
		{
			name:  "nothing says which",
			src:   "package p\n\n//go:generate go tool kanna-di\n//go:generate go tool kanna-validate\n",
			a:     answers{"p kanna-di": di, "p kanna-validate": validate},
			order: []string{"p/p.go:3 kanna-di", "p/p.go:4 kanna-validate"},
			says:  "order:  after 1 by position alone",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := makePlan(t, write(t, map[string]string{"p/p.go": tt.src}), tt.a)

			wantOrder(t, p, tt.order...)
			if got := written(p); !strings.Contains(got, tt.says) {
				t.Errorf("Write() =\n%s\nwant it to say %q", got, tt.says)
			}
		})
	}
}

// Code calling what two generators write has no order that compiles, and it
// need not: one runs ahead, told that the other's names are coming.
func TestMake_CodeCallsWhatBothGenerate(t *testing.T) {
	t.Parallel()

	dir := write(t, map[string]string{
		"p/p.go": "package p\n\n//go:generate go tool kanna-value\n//go:generate go tool kanna-validate\n\n" +
			"type In struct{}\n\nvar _, _ = NewEmail(), In{}.Validate()\n",
	})

	p := makePlan(t, dir, answers{
		"p kanna-value": {
			Packages: []string{"p"}, SourcesOnly: true, Writes: []string{"p/value_gen.go"}, Declares: []string{"p.NewEmail"},
		},
		"p kanna-validate": {
			Packages: []string{"p"}, Writes: []string{"p/validate_gen.go"}, Declares: []string{"p.In.Validate"},
		},
	})

	wantOrder(t, p, "p/p.go:3 kanna-value", "p/p.go:4 kanna-validate")
	if got, want := p.Tasks[0].Pending, []string{modulePath + "/p.In.Validate"}; !slices.Equal(got, want) {
		t.Errorf("Pending of the first = %q, want %q", got, want)
	}
	if got := p.Tasks[1].Pending; len(got) != 0 {
		t.Errorf("Pending of the last = %q, want none", got)
	}
	if got := written(p); !strings.Contains(got, "before: 2, which is told what it needs is coming") {
		t.Errorf("Write() =\n%s", got)
	}
}

// What is read through tests alone gives way too: go build has no such cycle.
func TestMake_ACycleThroughTests(t *testing.T) {
	t.Parallel()

	dir := write(t, map[string]string{
		"a/a.go":      "package a\n\n//go:generate go tool kanna-di\n",
		"a/a_test.go": "package a_test\n\nimport _ \"example.com/m/b\"\n",
		"b/b.go":      "package b\n\nimport _ \"example.com/m/a\"\n\n//go:generate go tool kanna-value\n",
	})

	p := makePlan(t, dir, answers{
		"a kanna-di": {
			Packages: []string{"a"}, Tests: true, Writes: []string{"a/di_gen.go"}, Declares: []string{"a.NewC"},
		},
		"b kanna-value": {
			Packages: []string{"b"}, Writes: []string{"b/value_gen.go"}, Declares: []string{"b.NewV"},
		},
	})

	wantOrder(t, p, "a/a.go:3 kanna-di", "b/b.go:5 kanna-value")
	if got, want := p.Tasks[0].Pending, []string{modulePath + "/b.NewV"}; !slices.Equal(got, want) {
		t.Errorf("Pending = %q, want %q", got, want)
	}
}

func TestMake_ACycleStops(t *testing.T) {
	t.Parallel()

	dir := write(t, map[string]string{
		"a/a.go": "package a\n\n//go:generate go tool kanna-di\n",
		"b/b.go": "package b\n\n//go:generate go tool kanna-validate\n",
	})

	p := makePlan(t, dir, answers{
		"a kanna-di": {
			Packages: []string{"a"},
			Refs:     []deps.Ref{{Package: "b", Via: "with=b.New"}},
			Writes:   []string{"a/di_gen.go"},
		},
		"b kanna-validate": {
			Packages: []string{"b"},
			Refs:     []deps.Ref{{Package: "a", Via: "rule=a.T"}},
			Writes:   []string{"b/validate_gen.go"},
		},
	})

	wantProblem(t, p, "wait for each other")
	wantProblem(t, p, "kanna-di at a/a.go:3 waits for kanna-validate at b/b.go:3: with=b.New names b")
	wantProblem(t, p, "kanna-validate at b/b.go:3 waits for kanna-di at a/a.go:3: rule=a.T names a")
	if len(p.Tasks) != 2 {
		t.Errorf("Tasks = %q, want both still shown", tasks(p))
	}
}

// The same line twice is the one run, as it would make no difference twice.
func TestMake_TheSameLineTwice(t *testing.T) {
	t.Parallel()

	dir := write(t, map[string]string{
		"p/a.go": "package p\n\n//go:generate go tool kanna-validate\n",
		"p/b.go": "package p\n\n//go:generate go tool kanna-validate\n",
	})

	p := makePlan(t, dir, answers{"p kanna-validate": {Packages: []string{"p"}, Writes: []string{"p/validate_gen.go"}}})

	wantOrder(t, p, "p/a.go:3 kanna-validate")
	if got := written(p); !strings.Contains(got, "also:   p/b.go:3") {
		t.Errorf("Write() =\n%s", got)
	}
}

// One tool twice over with other flags is two runs, of different output.
func TestMake_OneToolTwoInvocations(t *testing.T) {
	t.Parallel()

	dir := write(t, map[string]string{
		"p/p.go": "package p\n\n//go:generate go tool kanna-fixture -destination a\n" +
			"//go:generate go tool kanna-fixture -destination b\n",
	})

	p := makePlan(t, dir, answers{
		"p kanna-fixture -destination a": {Packages: []string{"p"}, Writes: []string{"p/a/fixture_gen.go"}},
		"p kanna-fixture -destination b": {Packages: []string{"p"}, Writes: []string{"p/b/fixture_gen.go"}},
	})

	wantOrder(t, p, "p/p.go:3 kanna-fixture", "p/p.go:4 kanna-fixture")
}

func TestMake_ConflictingOutput(t *testing.T) {
	t.Parallel()

	dir := write(t, map[string]string{
		"a/a.go": "package a\n\n//go:generate go tool kanna-fixture -destination ../shared\n",
		"b/b.go": "package b\n\n//go:generate go tool kanna-fixture -destination ../shared\n",
	})

	p := makePlan(t, dir, answers{
		"a kanna-fixture -destination ../shared": {Packages: []string{"a"}, Writes: []string{"shared/fixture_gen.go"}},
		"b kanna-fixture -destination ../shared": {Packages: []string{"b"}, Writes: []string{"shared/fixture_gen.go"}},
	})

	wantProblem(t, p, "kanna-fixture writes shared/fixture_gen.go, which kanna-fixture at a/a.go:3 writes too")
}

func TestMake_MissingInput(t *testing.T) {
	t.Parallel()

	dir := write(t, map[string]string{"p/p.go": "package p\n\n//go:generate go tool kanna-orm\n"})

	p := makePlan(t, dir, answers{"p kanna-orm": {Packages: []string{"p"}, Inputs: []string{"kanna-orm.snapshot.json"}}})

	wantProblem(t, p, "kanna-orm reads kanna-orm.snapshot.json, which is not there")
}

func TestMake_ExternalPackages(t *testing.T) {
	t.Parallel()

	files := map[string]string{
		"p/p.go":         "package p\n\nimport _ \"example.com/m/gen/pb\"\n\n//go:generate go tool kanna-mapper\n",
		"gen/pb/x.pb.go": "// Code generated by protoc-gen-go. DO NOT EDIT.\n\npackage pb\n",
	}
	a := answers{"p kanna-mapper": {Packages: []string{"p"}, Writes: []string{"p/mapper_gen.go"}}}

	dir := write(t, files)
	p := makePlan(t, dir, a)

	wantOrder(t, p, "p/p.go:5 kanna-mapper")
	if got := written(p); !strings.Contains(got, "external prerequisites:\n  gen/pb — generated by protoc-gen-go\n") {
		t.Errorf("Write() =\n%s", got)
	}

	if err := os.RemoveAll(filepath.Join(dir, "gen")); err != nil {
		t.Fatal(err)
	}
	wantProblem(t, makePlan(t, dir, a), "kanna-mapper reads gen/pb, which is not there and which no task writes")
}

func TestMake_LinesItDoesNotRun(t *testing.T) {
	t.Parallel()

	for _, line := range []string{
		"kanna-value",
		"go run github.com/go-kanna/value/cmd/kanna-value",
		"sh -c \"go tool kanna-value\"",
		"-command value go tool kanna-value",
		"go tool kanna-orm describe -database-url x",
		"go tool kanna gen",
		"go tool -n kanna-value",
		"kanna-value \"oops",
	} {
		t.Run(line, func(t *testing.T) {
			t.Parallel()

			dir := write(t, map[string]string{"p/p.go": "package p\n\n//go:generate " + line + "\n"})

			wantProblem(t, makePlan(t, dir, answers{}), "p/p.go:3")
		})
	}
}

// A package under the module's path that a module of its own holds is not one
// that is missing, whether it is checked out here or not.
func TestMake_ModulesUnderItsPath(t *testing.T) {
	t.Parallel()

	files := map[string]string{
		"p/p.go": "package p\n\nimport (\n\t_ \"example.com/m/api/v1\"\n\t_ \"example.com/m/nested\"\n)\n\n" +
			"//go:generate go tool kanna-value\n",
		"nested/go.mod": "module example.com/m/nested\n",
		"nested/n.go":   "package nested\n",
	}
	dir := write(t, files)
	gomod := "module example.com/m\n\ngo 1.26.0\n\nrequire example.com/m/api v1.0.0\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o600); err != nil {
		t.Fatal(err)
	}

	wantOrder(t, makePlan(t, dir, answers{"p kanna-value": {Packages: []string{"p"}}}), "p/p.go:8 kanna-value")
}

// A name that a package already read answers to means that one, however many
// others of the module share it.
func TestMake_ANameOfAPackageItImports(t *testing.T) {
	t.Parallel()

	dir := write(t, map[string]string{
		"a/use/u.go":   "package use\n\nimport _ \"example.com/m/a/model\"\n\n//go:generate go tool kanna-validate\n",
		"a/model/m.go": "package model\n",
		"b/model/m.go": "package model\n\nimport _ \"example.com/m/a/use\"\n\n//go:generate go tool kanna-value\n",
	})

	p := makePlan(t, dir, answers{
		"a/use kanna-validate": {
			Packages: []string{"a/use"},
			Refs:     []deps.Ref{{Package: "model", Via: "rule=model.T"}},
			Writes:   []string{"a/use/validate_gen.go"},
		},
		"b/model kanna-value": {Packages: []string{"b/model"}, Writes: []string{"b/model/value_gen.go"}},
	})

	wantOrder(t, p, "a/use/u.go:5 kanna-validate", "b/model/m.go:5 kanna-value")
}

// A package holding nothing but output is named by what it will be called.
func TestMake_ANameOfAPackageNotThereYet(t *testing.T) {
	t.Parallel()

	dir := write(t, map[string]string{
		"a/a.go": "package a\n\n//go:generate go tool kanna-di\n",
		"z/z.go": "package z\n\n//go:generate go tool kanna-orm -destination ../infra/pg\n",
	})

	p := makePlan(t, dir, answers{
		"a kanna-di": {
			Packages: []string{"a"},
			Refs:     []deps.Ref{{Package: "pg", Via: "with=pg.New"}},
			Writes:   []string{"a/di_gen.go"},
		},
		"z kanna-orm -destination ../infra/pg": {Packages: []string{"z"}, Writes: []string{"infra/pg/orm_gen.go"}},
	})

	wantOrder(t, p, "z/z.go:3 kanna-orm", "a/a.go:3 kanna-di")
}

func TestMake_IsTheSameEveryTime(t *testing.T) {
	t.Parallel()

	files := map[string]string{}
	a := answers{}
	for _, name := range []string{"a", "b", "c", "d", "e", "f"} {
		files[name+"/"+name+".go"] = "package " + name + "\n\n//go:generate go tool kanna-value\n" +
			"//go:generate go tool kanna-validate\n"
		a[name+" kanna-value"] = deps.Report{
			Packages: []string{name}, SourcesOnly: true, Writes: []string{name + "/value_gen.go"},
		}
		a[name+" kanna-validate"] = deps.Report{Packages: []string{name}, Writes: []string{name + "/validate_gen.go"}}
	}
	dir := write(t, files)

	first := written(makePlan(t, dir, a))
	for range 5 {
		if got := written(makePlan(t, dir, a)); got != first {
			t.Fatalf("got:\n%s\nfirst:\n%s", got, first)
		}
	}
}

// Lines of other generators are none of kanna's business.
func TestMake_OtherGenerators(t *testing.T) {
	t.Parallel()

	dir := write(t, map[string]string{
		"p/p.go": "package p\n\n//go:generate stringer -type=T\n//go:generate sh -c \"echo $GOFILE\n",
	})

	wantOrder(t, makePlan(t, dir, answers{}))
}

func TestMake_TheGeneratorCannotAnswer(t *testing.T) {
	t.Parallel()

	dir := write(t, map[string]string{"p/p.go": "package p\n\n//go:generate go tool kanna-value\n"})

	wantProblem(t, makePlan(t, dir, answers{}), `nothing answers "p kanna-value"`)
}

func TestMake_Patterns(t *testing.T) {
	t.Parallel()

	files := map[string]string{
		"low/l.go":  "package low\n\n//go:generate go tool kanna-value\n",
		"mid/m.go":  "package mid\n\nimport _ \"example.com/m/low\"\n\n//go:generate go tool kanna-validate\n",
		"high/h.go": "package high\n\nimport _ \"example.com/m/mid\"\n\n//go:generate go tool kanna-di\n",
		"else/e.go": "package other\n\n//go:generate go tool kanna-mapper\n",
		"src/s.go":  "package src\n\n//go:generate go tool kanna-fixture -destination ../out\n",
		"bad/b.go":  "package bad\n\n//go:generate sh -c \"go tool kanna-value\"\n",
	}
	a := answers{
		"low kanna-value":                       {Packages: []string{"low"}, Writes: []string{"low/value_gen.go"}},
		"mid kanna-validate":                    {Packages: []string{"mid"}, Writes: []string{"mid/validate_gen.go"}},
		"high kanna-di":                         {Packages: []string{"high"}, Writes: []string{"high/di_gen.go"}},
		"else kanna-mapper":                     {Packages: []string{"else"}, Writes: []string{"else/mapper_gen.go"}},
		"src kanna-fixture -destination ../out": {Packages: []string{"src"}, Writes: []string{"out/fixture_gen.go"}},
	}
	dir := write(t, files)

	// Producers come along, and those who read the result do not.
	wantOrder(t, makePlan(t, dir, a, "./mid"), "low/l.go:3 kanna-value", "mid/m.go:5 kanna-validate")
	// A package holding output is matched by what writes it.
	wantOrder(t, makePlan(t, dir, a, "./out"), "src/s.go:3 kanna-fixture")
	wantProblem(t, makePlan(t, dir, a, "./none"), "no //go:generate go tool kanna-* line matches ./none")
	wantProblem(t, makePlan(t, dir, a, "./bad"), "bad/b.go:3")
}

// The plan is the same before generators have run and after, with old output
// that does not even compile among it.
func TestMake_OutputHasNoSay(t *testing.T) {
	t.Parallel()

	files := map[string]string{
		"a/a.go": "package a\n\nimport _ \"example.com/m/z\"\n\n//go:generate go tool kanna-validate\n" +
			"//go:generate go tool kanna-di\n\nvar _ = In{}.Validate()\n\ntype In struct{}\n",
		"z/z.go": "package z\n\n//go:generate go tool kanna-value\n",
	}
	a := answers{
		"a kanna-validate": {
			Packages: []string{"a"}, Writes: []string{"a/validate_gen.go"}, Declares: []string{"a.In.Validate"},
		},
		"a kanna-di":    {Packages: []string{"a"}, Writes: []string{"a/di_gen.go"}, Declares: []string{"a.NewC"}},
		"z kanna-value": {Packages: []string{"z"}, Writes: []string{"z/value_gen.go"}},
	}
	fresh := written(makePlan(t, write(t, files), a))

	files["a/validate_gen.go"] = "// Code generated by kanna-validate v0.0.1. DO NOT EDIT.\n\npackage a\n\n" +
		"import _ \"example.com/m/gone\"\n\nfunc (In) Validate() error { return nil }\n"
	files["a/di_gen.go"] = "// Code generated by kanna-di v0.0.1. DO NOT EDIT.\n\npackage stale\n\nfunc broken( {\n"
	files["z/value_gen.go"] = "// Code generated by kanna-value v0.0.1. DO NOT EDIT.\n\npackage z\n\n" +
		"import _ \"example.com/m/a\"\n"

	if got := written(makePlan(t, write(t, files), a)); got != fresh {
		t.Errorf("with output:\n%s\nwithout:\n%s", got, fresh)
	}
}

func TestAsk_NoSuchTool(t *testing.T) {
	t.Parallel()

	dir := write(t, map[string]string{"p/p.go": "package p\n\n//go:generate go tool kanna-value\n"})
	m, err := module.Find(dir, build.Default)
	if err != nil {
		t.Fatal(err)
	}
	selection, err := m.Select(dir, nil)
	if err != nil {
		t.Fatal(err)
	}

	ctxt := build.Default
	ctxt.GOROOT = goroot(t)
	p, err := plan.Make(t.Context(), plan.Config{
		Module: m, Selection: selection, Build: ctxt, Ask: plan.Ask(ctxt),
	})
	if err != nil {
		t.Fatal(err)
	}

	wantProblem(t, p, "kanna-value is not a tool of this module")
	wantProblem(t, p, "go get -tool github.com/go-kanna/value/cmd/kanna-value@latest")
}
