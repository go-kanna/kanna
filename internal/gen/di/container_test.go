package di_test

import (
	"strings"
	"testing"

	"github.com/go-kanna/kanna/internal/diag"
	"github.com/go-kanna/kanna/internal/gen/di"
	"github.com/go-kanna/kanna/internal/internaltest"
	"github.com/go-kanna/kanna/internal/packages"
	"github.com/go-kanna/kanna/internal/scan"
)

func TestContainers_DetectsTaggedStruct(t *testing.T) {
	t.Parallel()

	src := `package test

type DB struct{}
type User struct{}

type Container struct {
	User *User ` + "`di:\"\"`" + `
}
`
	containers, ds := containersOf(t, src)
	assertNoErrors(t, ds)

	if got, want := len(containers), 1; got != want {
		t.Fatalf("containers = %d, want %d", got, want)
	}

	c := containers[0]
	if c.StructName != "Container" {
		t.Errorf("StructName = %q, want %q", c.StructName, "Container")
	}
	if c.PkgPath != "test" || c.PkgName != "test" {
		t.Errorf("PkgPath, PkgName = %q, %q; want %q, %q", c.PkgPath, c.PkgName, "test", "test")
	}
	if c.Named == nil || c.Named.Obj().Name() != "Container" {
		t.Errorf("Named = %v, want the Container type", c.Named)
	}
	if got, want := len(c.Fields), 1; got != want {
		t.Fatalf("fields = %d, want %d", got, want)
	}
	if c.Fields[0].Name != "User" || c.Fields[0].Role != di.RoleOut {
		t.Errorf("field = %+v, want User with RoleOut", c.Fields[0])
	}
}

func TestContainers_SkipsStructsWithoutDITags(t *testing.T) {
	t.Parallel()

	src := `package test

type Plain struct {
	Name string
}

type Tagged struct {
	Name string ` + "`json:\"name\"`" + `
}
`
	containers, ds := containersOf(t, src)
	assertNoErrors(t, ds)

	if len(containers) != 0 {
		t.Errorf("containers = %d, want 0", len(containers))
	}
}

func TestContainers_ReportsDirectiveWithoutTaggedField(t *testing.T) {
	t.Parallel()

	// Skipping a struct with no di tag is right, but an explicit directive means
	// the author expected a container, so dropping it silently hides a mistake.
	src := `package test

//kanna:container name=NewApp
type app struct {
	Name string
}
`
	containers, ds := containersOf(t, src)

	assertErrorContains(t, ds, "//kanna:container on app but no field carries a di tag")
	if len(containers) != 0 {
		t.Errorf("containers = %d, want 0", len(containers))
	}
}

func TestContainers_IgnoresUntaggedFields(t *testing.T) {
	t.Parallel()

	src := `package test

type User struct{}

type Container struct {
	User    *User ` + "`di:\"\"`" + `
	Version string
	private int
}
`
	containers, ds := containersOf(t, src)
	assertNoErrors(t, ds)

	if got, want := len(containers[0].Fields), 1; got != want {
		t.Fatalf("fields = %d, want %d (only the tagged one)", got, want)
	}
}

func TestContainers_AssignsRoles(t *testing.T) {
	t.Parallel()

	src := `package test

type Greeter interface{ Greet() string }
type DB struct{}
type Config struct{ Addr string }

type Container struct {
	Out      Greeter ` + "`di:\"\"`" + `
	_        DB      ` + "`di:\"with=NewWriter\"`" + `
	Selected DB      ` + "`di:\"with=NewReader\"`" + `
	_        Greeter ` + "`di:\"arg\"`" + `
	Named    Greeter ` + "`di:\"arg=primary\"`" + `
	Returned Greeter ` + "`di:\"returns\"`" + `
	_        Config  ` + "`di:\"embed\"`" + `
}
`
	containers, ds := containersOf(t, src)
	assertNoErrors(t, ds)

	fields := containers[0].Fields
	want := []struct {
		name    string
		role    di.Role
		ref     string
		argName string
		returns bool
	}{
		{name: "Out", role: di.RoleOut},
		{name: "_", role: di.RoleOverride, ref: "NewWriter"},
		{name: "Selected", role: di.RoleOut, ref: "NewReader"},
		{name: "_", role: di.RoleArg},
		{name: "Named", role: di.RoleArg, argName: "primary"},
		{name: "Returned", role: di.RoleOut, returns: true},
		{name: "_", role: di.RoleEmbed},
	}

	if len(fields) != len(want) {
		t.Fatalf("fields = %d, want %d", len(fields), len(want))
	}
	for i, w := range want {
		got := fields[i]
		if got.Name != w.name || got.Role != w.role {
			t.Errorf("field %d = (%q, %v), want (%q, %v)", i, got.Name, got.Role, w.name, w.role)
		}
		if got.ProviderRef.Raw != w.ref {
			t.Errorf("field %d ProviderRef = %q, want %q", i, got.ProviderRef.Raw, w.ref)
		}
		if got.ArgName != w.argName {
			t.Errorf("field %d ArgName = %q, want %q", i, got.ArgName, w.argName)
		}
		if got.IsReturns != w.returns {
			t.Errorf("field %d IsReturns = %v, want %v", i, got.IsReturns, w.returns)
		}
	}
}

func TestContainers_AssignsReturnsOnlyRole(t *testing.T) {
	t.Parallel()

	// A blank returns field declares the constructor's return type without
	// contributing a value to the struct literal.
	src := `package test

type Greeter interface{ Greet() string }
type DB struct{}

type Container struct {
	_  Greeter ` + "`di:\"returns\"`" + `
	DB DB      ` + "`di:\"\"`" + `
}
`
	containers, ds := containersOf(t, src)
	assertNoErrors(t, ds)

	fields := containers[0].Fields
	if got, want := len(fields), 2; got != want {
		t.Fatalf("fields = %d, want %d", got, want)
	}
	if fields[0].Role != di.RoleReturnsOnly {
		t.Errorf("field 0 Role = %v, want RoleReturnsOnly", fields[0].Role)
	}
	if !fields[0].IsReturns {
		t.Error("field 0 IsReturns = false, want true")
	}
}

func TestContainers_RejectsBlankMarker(t *testing.T) {
	t.Parallel()

	src := `package test

type DB struct{}

type Container struct {
	_    DB ` + "`di:\"\"`" + `
	Real DB ` + "`di:\"\"`" + `
}
`
	containers, ds := containersOf(t, src)

	// The message must list every form that is legal on a blank field.
	assertErrorContains(t, ds, `_ field requires di:"with=...", di:"arg", di:"returns" or di:"embed"`)
	// The valid field still forms a container so the rest of the model survives.
	if got, want := len(containers), 1; got != want {
		t.Fatalf("containers = %d, want %d", got, want)
	}
	if got, want := len(containers[0].Fields), 1; got != want {
		t.Errorf("fields = %d, want %d", got, want)
	}
}

func TestContainers_RejectsNonBlankEmbedTag(t *testing.T) {
	t.Parallel()

	src := `package test

type Config struct{ Addr string }

type Container struct {
	Cfg  Config ` + "`di:\"embed\"`" + `
	Real Config ` + "`di:\"\"`" + `
}
`
	_, ds := containersOf(t, src)

	assertErrorContains(t, ds, `di:"embed" requires a blank field (_)`)
}

func TestContainers_RejectsEmbeddedFieldWithDITag(t *testing.T) {
	t.Parallel()

	src := `package test

type Base struct{ ID string }

type Container struct {
	Base ` + "`di:\"\"`" + `
}
`
	containers, ds := containersOf(t, src)

	assertErrorContains(t, ds, "embedded field with di tag is not supported")
	if len(containers) != 0 {
		t.Errorf("containers = %d, want 0 (no usable fields remain)", len(containers))
	}
}

func TestContainers_ReportsMalformedTag(t *testing.T) {
	t.Parallel()

	src := `package test

type DB struct{}

type Container struct {
	_    DB ` + "`di:\"with=\"`" + `
	Real DB ` + "`di:\"\"`" + `
}
`
	_, ds := containersOf(t, src)

	assertErrorContains(t, ds, `di:"with=..." requires a provider reference`)
}

func TestContainers_AppliesDirective(t *testing.T) {
	t.Parallel()

	src := `package test

type Greeter interface{ Greet() string }

//kanna:container name=NewApp must
type app struct {
	G Greeter ` + "`di:\"\"`" + `
}
`
	containers, ds := containersOf(t, src)
	assertNoErrors(t, ds)

	d := containers[0].Directive
	if d.Name != "NewApp" {
		t.Errorf("Directive.Name = %q, want %q", d.Name, "NewApp")
	}
	if d.Must != di.MustOn {
		t.Errorf("Directive.Must = %v, want MustOn", d.Must)
	}
	if d.ReturnType != nil {
		t.Errorf("Directive.ReturnType = %v, want nil", d.ReturnType)
	}
}

func TestContainers_ResolvesLocalReturnType(t *testing.T) {
	t.Parallel()

	src := `package test

type Greeter interface{ Greet() string }

//kanna:container returns=Greeter
type app struct {
	G Greeter ` + "`di:\"\"`" + `
}
`
	containers, ds := containersOf(t, src)
	assertNoErrors(t, ds)

	got := containers[0].Directive.ReturnType
	if got == nil {
		t.Fatal("Directive.ReturnType = nil, want test.Greeter")
	}
	if want := "test.Greeter"; got.String() != want {
		t.Errorf("Directive.ReturnType = %q, want %q", got.String(), want)
	}
}

func TestContainers_ResolvesQualifiedReturnType(t *testing.T) {
	t.Parallel()

	// A qualified name resolves only when the expression is evaluated at a
	// position inside the declaring file, since imports live in file scope.
	src := `package test

import "io"

//kanna:container returns=io.Writer
type app struct {
	W io.Writer ` + "`di:\"\"`" + `
}
`
	containers, ds := containersOf(t, src)
	assertNoErrors(t, ds)

	got := containers[0].Directive.ReturnType
	if got == nil {
		t.Fatal("Directive.ReturnType = nil, want io.Writer")
	}
	if want := "io.Writer"; got.String() != want {
		t.Errorf("Directive.ReturnType = %q, want %q", got.String(), want)
	}
}

func TestContainers_ReportsUnresolvableReturnType(t *testing.T) {
	t.Parallel()

	src := `package test

type Greeter interface{ Greet() string }

//kanna:container returns=Missing
type app struct {
	G Greeter ` + "`di:\"\"`" + `
}
`
	containers, ds := containersOf(t, src)

	assertErrorContains(t, ds, "directive returns=Missing")
	// The container is still reported, only without a resolved return type.
	if got, want := len(containers), 1; got != want {
		t.Fatalf("containers = %d, want %d", got, want)
	}
	if containers[0].Directive.ReturnType != nil {
		t.Error("Directive.ReturnType was set despite the resolution failure")
	}
}

func TestContainers_ReportsDirectiveSyntaxErrors(t *testing.T) {
	t.Parallel()

	src := `package test

type Greeter interface{ Greet() string }

//kanna:container xyz=1
type app struct {
	G Greeter ` + "`di:\"\"`" + `
}
`
	_, ds := containersOf(t, src)

	assertErrorContains(t, ds, `unknown directive key "xyz"`)
}

func TestContainers_ReportsMissingFileSet(t *testing.T) {
	t.Parallel()

	src := `package test

type Greeter interface{ Greet() string }

//kanna:container returns=Greeter
type app struct {
	G Greeter ` + "`di:\"\"`" + `
}
`
	pkg := internaltest.LoadFile(t, src)
	structs, scanDiags := scan.Structs([]*packages.Package{pkg})
	assertNoErrors(t, scanDiags)

	containers, ds := di.Containers(nil, structs)

	assertErrorContains(t, ds, "file set or package unavailable")
	if got, want := len(containers), 1; got != want {
		t.Fatalf("containers = %d, want %d", got, want)
	}
}

func TestContainers_PreservesDeclarationOrder(t *testing.T) {
	t.Parallel()

	src := `package test

type DB struct{}

type Zebra struct {
	DB DB ` + "`di:\"\"`" + `
}

type Apple struct {
	DB DB ` + "`di:\"\"`" + `
}
`
	containers, ds := containersOf(t, src)
	assertNoErrors(t, ds)

	names := make([]string, 0, len(containers))
	for _, c := range containers {
		names = append(names, c.StructName)
	}
	if want := []string{"Zebra", "Apple"}; len(names) != 2 || names[0] != want[0] || names[1] != want[1] {
		t.Errorf("containers = %v, want %v", names, want)
	}
}

func TestContainers_SkipsGeneratedFiles(t *testing.T) {
	t.Parallel()

	pkg := internaltest.LoadPackage(t, map[string]string{
		"a_handwritten.go": `package test

type DB struct{}

type Handwritten struct {
	DB DB ` + "`di:\"\"`" + `
}
`,
		"b_generated.go": `// Code generated by kanna. DO NOT EDIT.

package test

type Generated struct {
	DB DB ` + "`di:\"\"`" + `
}
`,
	})

	structs, scanDiags := scan.Structs([]*packages.Package{pkg})
	assertNoErrors(t, scanDiags)

	containers, ds := di.Containers(pkg.Fset, structs)
	assertNoErrors(t, ds)

	if got, want := len(containers), 1; got != want {
		t.Fatalf("containers = %d, want %d", got, want)
	}
	if containers[0].StructName != "Handwritten" {
		t.Errorf("StructName = %q, want %q", containers[0].StructName, "Handwritten")
	}
}

// containersOf runs the full scan then Containers pipeline over a single source
// file, which is how the generator itself composes the two layers.
func containersOf(t *testing.T, src string) ([]di.Container, []diag.Diag) {
	t.Helper()

	pkg := internaltest.LoadFile(t, src)

	structs, ds := scan.Structs([]*packages.Package{pkg})
	if diag.HasErrors(ds) {
		t.Fatalf("scan reported errors: %s", diag.Format(ds))
	}

	return di.Containers(pkg.Fset, structs)
}

func assertNoErrors(t *testing.T, ds []diag.Diag) {
	t.Helper()

	if diag.HasErrors(ds) {
		t.Fatalf("unexpected error diagnostics: %s", diag.Format(ds))
	}
}

func assertErrorContains(t *testing.T, ds []diag.Diag, want string) {
	t.Helper()

	if !diag.HasErrors(ds) {
		t.Fatalf("no error diagnostics; want one containing %q", want)
	}
	if got := diag.Format(ds); !strings.Contains(got, want) {
		t.Errorf("diagnostics = %q, want one containing %q", got, want)
	}
}
