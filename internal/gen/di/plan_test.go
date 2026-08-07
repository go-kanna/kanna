package di_test

import (
	"strings"
	"testing"

	"github.com/go-kanna/kanna/internal/diag"
	"github.com/go-kanna/kanna/internal/gen/di"
	"github.com/go-kanna/kanna/internal/packages"
	"github.com/go-kanna/kanna/internal/pkgtest"
	"github.com/go-kanna/kanna/internal/scan"
)

func TestBuild_SimpleChain(t *testing.T) {
	t.Parallel()

	src := `package test
type DB struct{}
type User struct{}
func NewDB() *DB { return nil }
func NewUser(db *DB) *User { return nil }
type Container struct {
	User *User ` + "`di:\"\"`" + `
}
`
	p, _ := mustBuild(t, src, "Container", di.Options{})

	if got, want := p.ConstructorName, "NewContainer"; got != want {
		t.Errorf("ConstructorName = %q, want %q", got, want)
	}
	if len(p.Steps) != 2 {
		t.Fatalf("steps = %d, want 2", len(p.Steps))
	}
	if p.Steps[0].Provider == nil || p.Steps[0].Provider.FuncName != "NewDB" {
		t.Errorf("step[0] = %+v, want NewDB", p.Steps[0])
	}
	if p.Steps[1].Provider == nil || p.Steps[1].Provider.FuncName != "NewUser" {
		t.Errorf("step[1] = %+v, want NewUser", p.Steps[1])
	}
	if len(p.Outputs) != 1 || p.Outputs[0].FieldName != "User" {
		t.Errorf("outputs = %+v", p.Outputs)
	}
	if p.ReturnsError {
		t.Errorf("ReturnsError = true, want false")
	}
}

func TestBuild_WithErrorPropagates(t *testing.T) {
	t.Parallel()

	src := `package test
type DB struct{}
func NewDB() (*DB, error) { return nil, nil }
type Container struct {
	DB *DB ` + "`di:\"\"`" + `
}
`
	p, _ := mustBuild(t, src, "Container", di.Options{})
	if !p.ReturnsError {
		t.Errorf("ReturnsError = false, want true")
	}
}

func TestBuild_ArgInput(t *testing.T) {
	t.Parallel()

	src := `package test
type DB struct{}
type User struct{}
func NewUser(db *DB) *User { return nil }
type Container struct {
	_    *DB   ` + "`di:\"arg\"`" + `
	User *User ` + "`di:\"\"`" + `
}
`
	p, _ := mustBuild(t, src, "Container", di.Options{})

	if len(p.Inputs) != 1 || p.Inputs[0].Name != "db" {
		t.Errorf("inputs = %+v", p.Inputs)
	}

	if p.Steps[0].Kind != di.StepKindInput {
		t.Errorf("step[0].Kind = %v, want StepKindInput", p.Steps[0].Kind)
	}
}

func TestBuild_ArgWithCustomName(t *testing.T) {
	t.Parallel()

	src := `package test
type DB struct{}
func NewUserish(db *DB) *DB { return nil }
type Container struct {
	_  *DB ` + "`di:\"arg=primary\"`" + `
	DB *DB ` + "`di:\"with=NewUserish\"`" + `
}
`
	p, _ := mustBuild(t, src, "Container", di.Options{})

	if len(p.Inputs) != 1 || p.Inputs[0].Name != "primary" {
		t.Errorf("inputs = %+v, want name=primary", p.Inputs)
	}
}

func TestBuild_OverrideAmbiguous(t *testing.T) {
	t.Parallel()

	src := `package test
type DB struct{}
type User struct{}
func NewWriter() *DB { return nil }
func NewReader() *DB { return nil }
func NewUser(db *DB) *User { return nil }
type Container struct {
	_    *DB   ` + "`di:\"with=NewWriter\"`" + `
	User *User ` + "`di:\"\"`" + `
}
`
	p, _ := mustBuild(t, src, "Container", di.Options{})

	var sawWriter bool
	for _, s := range p.Steps {
		if s.Provider == nil {
			continue
		}
		switch s.Provider.FuncName {
		case "NewWriter":
			sawWriter = true
		case "NewReader":
			t.Errorf("NewReader should not appear in the plan when override is NewWriter")
		case "NewUser":
			if !sawWriter {
				t.Errorf("NewWriter must precede NewUser, got steps: %+v", p.Steps)
			}
		}
	}
	if !sawWriter {
		t.Errorf("expected NewWriter step, got %+v", p.Steps)
	}
}

func TestBuild_FieldBoundStepUsesFieldName(t *testing.T) {
	t.Parallel()

	// `Tx Transactor di:""` should produce a variable named after the
	// destination field — "tx" — rather than the function ("new") or the
	// result type ("transactor").
	src := `package test
type Transactor struct{}
func New() Transactor { return Transactor{} }
type Container struct {
	Tx Transactor ` + "`di:\"\"`" + `
}
`
	p, _ := mustBuild(t, src, "Container", di.Options{})

	if len(p.Steps) != 1 {
		t.Fatalf("steps = %d, want 1", len(p.Steps))
	}
	if got, want := p.Steps[0].VarName, "tx"; got != want {
		t.Errorf("var name = %q, want %q", got, want)
	}
}

func TestBuild_IntermediateStepUsesResultType(t *testing.T) {
	t.Parallel()

	// `New() *Foo` consumed transitively by `Make` (whose result is the
	// container's field) is an intermediate step with no field name to
	// borrow from. It should fall back to the result type — "foo" — not
	// the function name.
	src := `package test
type Foo struct{}
type Bar struct{}
func New() *Foo { return nil }
func Make(f *Foo) *Bar { return nil }
type Container struct {
	Bar *Bar ` + "`di:\"\"`" + `
}
`
	p, _ := mustBuild(t, src, "Container", di.Options{})

	var fooStep, barStep di.Step
	for _, s := range p.Steps {
		switch {
		case s.Provider != nil && s.Provider.FuncName == "New":
			fooStep = s
		case s.Provider != nil && s.Provider.FuncName == "Make":
			barStep = s
		}
	}
	if got, want := fooStep.VarName, "foo"; got != want {
		t.Errorf("intermediate var name = %q, want %q", got, want)
	}
	if got, want := barStep.VarName, "bar"; got != want {
		t.Errorf("field-bound var name = %q, want %q", got, want)
	}
}

func TestBuild_FieldNameSwapNoUnnecessarySuffix(t *testing.T) {
	t.Parallel()

	// Two steps want to swap names: step A holds "foo" (result type Foo)
	// and is bound to a "Db" field (wants "db"), while step B holds "db"
	// (result type Db) and is bound to a "Foo" field (wants "foo"). The
	// rename pass must vacate both old names before picking the new ones
	// so each side gets its preferred name without a `_2` suffix.
	src := `package test
type Foo struct{}
type Db struct{}
func NewFoo() Foo { return Foo{} }
func NewDb() Db   { return Db{} }
type Container struct {
	Db  Foo ` + "`di:\"\"`" + `
	Foo Db  ` + "`di:\"\"`" + `
}
`
	p, _ := mustBuild(t, src, "Container", di.Options{})

	bindings := map[string]string{}
	for _, o := range p.Outputs {
		bindings[o.FieldName] = p.Steps[o.StepIndex].VarName
	}
	if got, want := bindings["Db"], "db"; got != want {
		t.Errorf("field Db var = %q, want %q (swap should not force a suffix)", got, want)
	}
	if got, want := bindings["Foo"], "foo"; got != want {
		t.Errorf("field Foo var = %q, want %q (swap should not force a suffix)", got, want)
	}
}

func TestBuild_TypeAliasResultDerivesAliasName(t *testing.T) {
	t.Parallel()

	// A provider returning a type alias used to fall through deriveInputName's
	// *types.Named check (alias is *types.Alias in Go 1.22+), leaving the
	// variable to fall back to the function name. After types.Unalias is
	// applied inside deriveInputName, the alias's target name flows through.
	src := `package test
type Real struct{}
type Alias = Real
func New() Alias { return Alias{} }
type Container struct {
	V Alias ` + "`di:\"\"`" + `
}
`
	p, _ := mustBuild(t, src, "Container", di.Options{})

	// Steps are: one provider step (New). It's field-bound to V → renamed
	// to lowerFirst("V") = "v". The interesting check is the intermediate
	// case: if no field were attached, we'd want "real" not "new" / "arg".
	// Reuse the existing intermediate-naming test shape:
	src2 := `package test
type Real struct{}
type Alias = Real
type Wrap struct{}
func New() Alias        { return Alias{} }
func Wrapper(a Alias) Wrap { return Wrap{} }
type Container struct {
	W Wrap ` + "`di:\"\"`" + `
}
`
	p2, _ := mustBuild(t, src2, "Container", di.Options{})

	var aliasStep di.Step
	for _, s := range p2.Steps {
		if s.Provider != nil && s.Provider.FuncName == "New" {
			aliasStep = s
			break
		}
	}
	if got, want := aliasStep.VarName, "real"; got != want {
		t.Errorf("alias intermediate var = %q, want %q (Unalias should resolve to Real)", got, want)
	}

	// Sanity: the field-bound case in p (V Alias) lands on "v" through
	// the rename pass.
	if p.Outputs[0].FieldName != "V" || p.Steps[p.Outputs[0].StepIndex].VarName != "v" {
		t.Errorf("field-bound alias did not land on field name; outputs=%+v steps=%+v",
			p.Outputs, p.Steps)
	}
}

func TestBuild_SharedStepDoesNotLeakCandidate(t *testing.T) {
	t.Parallel()

	// Two container fields share the same provider step (both want *DB).
	// The naive rename pass would queue the step twice — once with "db"
	// and once with "backup" — and mark both names as used, forcing an
	// unrelated step that wanted "backup" onto "backup2". The decided-
	// once gate ensures only the first field name is queued.
	src := `package test
type DB struct{}
type Backup struct{}
func NewDB() *DB         { return nil }
func NewBackup() *Backup { return nil }
type Container struct {
	DB     *DB     ` + "`di:\"\"`" + `
	Backup *Backup ` + "`di:\"\"`" + `
	Twin   *DB     ` + "`di:\"\"`" + `
}
`
	p, _ := mustBuild(t, src, "Container", di.Options{})

	var backupVar string
	for _, s := range p.Steps {
		if s.Provider != nil && s.Provider.FuncName == "NewBackup" {
			backupVar = s.VarName
			break
		}
	}
	if got, want := backupVar, "backup"; got != want {
		t.Errorf("NewBackup var = %q, want %q (Twin sharing *DB must not leak the name)", got, want)
	}
}

func TestBuild_SharedStepPreservesMatchingFieldName(t *testing.T) {
	t.Parallel()

	// Step's existing name "db" already matches field "DB"; another
	// field "Backup" also binds to the same step. The first match
	// should leave the variable alone instead of subsequently renaming
	// it to "backup".
	src := `package test
type DB struct{}
func NewDB() *DB { return nil }
type Container struct {
	DB     *DB ` + "`di:\"\"`" + `
	Backup *DB ` + "`di:\"\"`" + `
}
`
	p, _ := mustBuild(t, src, "Container", di.Options{})

	if len(p.Steps) != 1 {
		t.Fatalf("steps = %d, want 1", len(p.Steps))
	}
	if got, want := p.Steps[0].VarName, "db"; got != want {
		t.Errorf("shared-step var = %q, want %q (matching field name should win)", got, want)
	}
}

func TestBuild_SelfGeneratedProviderIgnored(t *testing.T) {
	t.Parallel()

	// A `di:"returns"` container whose previously generated
	// constructor is now visible to the provider scan must not pick that
	// constructor up as a candidate when resolving its own field, or it
	// would loop forever. The unrelated, non-self provider remains usable.
	src := `package test
type Greeter interface{ Greet() string }
type greeterImpl struct{}
func (greeterImpl) Greet() string { return "" }
func NewGreeterImpl() Greeter { return greeterImpl{} }

// Pretend this came from a previous generation of NewWrapper.
func NewWrapper() Greeter { return nil }

type wrapper struct {
	service Greeter ` + "`di:\"returns\"`" + `
}
`
	p, ds := build(t, src, "wrapper", di.Options{})
	if diag.HasErrors(ds) {
		t.Fatalf("expected the self provider NewWrapper to be filtered out, got %v", ds)
	}
	if len(p.Steps) != 1 || p.Steps[0].Provider == nil || p.Steps[0].Provider.FuncName != "NewGreeterImpl" {
		t.Errorf("expected NewGreeterImpl to be the only provider step; got %+v", p.Steps)
	}
}

func TestBuild_FieldNameTakenForcesSuffix(t *testing.T) {
	t.Parallel()

	// An intermediate step's natural name ("tx", from result type Tx)
	// already occupies that slot, then a field-bound step (field "Tx",
	// holding a different type) tries to claim "tx" too. The field-bound
	// step should cascade to "tx2" — the simple-cascade behavior chosen
	// in option A.
	src := `package test
type Tx struct{}
type Other struct{}
func NewTx() Tx { return Tx{} }
func NewOther(Tx) Other { return Other{} }
type Container struct {
	Tx Other ` + "`di:\"\"`" + `
}
`
	p, _ := mustBuild(t, src, "Container", di.Options{})

	var intermediate, fieldBound di.Step
	for _, s := range p.Steps {
		switch {
		case s.Provider != nil && s.Provider.FuncName == "NewTx":
			intermediate = s
		case s.Provider != nil && s.Provider.FuncName == "NewOther":
			fieldBound = s
		}
	}
	if got, want := intermediate.VarName, "tx"; got != want {
		t.Errorf("intermediate var = %q, want %q", got, want)
	}
	if got, want := fieldBound.VarName, "tx2"; got != want {
		t.Errorf("field-bound var = %q, want %q (cascade)", got, want)
	}
}

func TestBuild_NonBlankWithActsAsOverride(t *testing.T) {
	t.Parallel()

	src := `package test
type DB struct{}
type User struct{}
func NewWriter() *DB { return nil }
func NewReader() *DB { return nil }
func NewUser(db *DB) *User { return nil }
type Container struct {
	DB   *DB   ` + "`di:\"with=NewWriter\"`" + `
	User *User ` + "`di:\"\"`" + `
}
`
	p, _ := mustBuild(t, src, "Container", di.Options{})

	for _, s := range p.Steps {
		if s.Provider != nil && s.Provider.FuncName == "NewReader" {
			t.Errorf("NewReader must not appear when DB is pinned to NewWriter; steps=%+v", p.Steps)
		}
	}

	var hasDBOutput, hasUserOutput bool
	for _, o := range p.Outputs {
		switch o.FieldName {
		case "DB":
			hasDBOutput = true
		case "User":
			hasUserOutput = true
		}
	}
	if !hasDBOutput || !hasUserOutput {
		t.Errorf("expected both DB and User outputs, got %+v", p.Outputs)
	}
}

func TestBuild_NonBlankWithConflictingProviders(t *testing.T) {
	t.Parallel()

	src := `package test
type DB struct{}
func NewWriter() *DB { return nil }
func NewReader() *DB { return nil }
type Container struct {
	A *DB ` + "`di:\"with=NewWriter\"`" + `
	B *DB ` + "`di:\"with=NewReader\"`" + `
}
`
	_, ds := build(t, src, "Container", di.Options{})
	if !diag.HasErrors(ds) {
		t.Fatalf("expected conflicting-providers diag, got %v", ds)
	}
}

func TestBuild_AmbiguousProviderError(t *testing.T) {
	t.Parallel()

	src := `package test
type DB struct{}
func NewWriter() *DB { return nil }
func NewReader() *DB { return nil }
type Container struct {
	DB *DB ` + "`di:\"\"`" + `
}
`
	p, ds := build(t, src, "Container", di.Options{})
	if !diag.HasErrors(ds) {
		t.Fatalf("expected ambiguity error, got plan=%+v diags=%v", p, ds)
	}
}

func TestBuild_NoProviderError(t *testing.T) {
	t.Parallel()

	src := `package test
type DB struct{}
type Container struct {
	DB *DB ` + "`di:\"\"`" + `
}
`
	p, ds := build(t, src, "Container", di.Options{})
	if !diag.HasErrors(ds) {
		t.Fatalf("expected missing provider error, got plan=%+v diags=%v", p, ds)
	}
}

func TestBuild_DirectiveReturnTypeOverridesPointer(t *testing.T) {
	t.Parallel()

	src := `package test
type Greeter interface{ Greet() string }
type greeterImpl struct{}
func (greeterImpl) Greet() string { return "" }
func NewImpl() *greeterImpl { return nil }

//kanna:container returns=Greeter
type app struct {
	G *greeterImpl ` + "`di:\"\"`" + `
}
`
	p, _ := mustBuild(t, src, "app", di.Options{})
	if p.ReturnType == nil {
		t.Fatal("ReturnType is nil")
	}
	if got := di.TypeString(p.ReturnType); got != "test.Greeter" {
		t.Errorf("ReturnType = %s, want test.Greeter", got)
	}
}

func TestBuild_TagReturnsSetsReturnType(t *testing.T) {
	t.Parallel()

	src := `package test
type Greeter interface{ Greet() string }
type greeterImpl struct{}
func (greeterImpl) Greet() string { return "" }
func NewImpl() *greeterImpl { return nil }

type app struct {
	G *greeterImpl ` + "`di:\"returns\"`" + `
}
`
	p, _ := mustBuild(t, src, "app", di.Options{})
	if p.ReturnType == nil {
		t.Fatal("ReturnType is nil")
	}
	if got := di.TypeString(p.ReturnType); got != "*test.greeterImpl" {
		t.Errorf("ReturnType = %s, want *test.greeterImpl", got)
	}
	if len(p.Outputs) != 1 || p.Outputs[0].FieldName != "G" {
		t.Errorf("outputs = %+v", p.Outputs)
	}
}

func TestBuild_DirectiveAndTagReturnsConflict(t *testing.T) {
	t.Parallel()

	src := `package test
type Greeter interface{ Greet() string }
type greeterImpl struct{}
func (greeterImpl) Greet() string { return "" }
func NewImpl() *greeterImpl { return nil }

//kanna:container returns=Greeter
type app struct {
	_ Greeter        ` + "`di:\"returns\"`" + `
	G *greeterImpl  ` + "`di:\"\"`" + `
}
`
	_, ds := build(t, src, "app", di.Options{})
	if !diag.HasErrors(ds) {
		t.Fatalf("expected conflict error, got %v", ds)
	}
}

func TestBuild_DuplicateArgName(t *testing.T) {
	t.Parallel()

	src := `package test
type DB struct{}
type Cache struct{}
type Container struct {
	_  *DB    ` + "`di:\"arg=primary\"`" + `
	_  *Cache ` + "`di:\"arg=primary\"`" + `
	DB *DB    ` + "`di:\"\"`" + `
}
`
	_, ds := build(t, src, "Container", di.Options{})
	if !diag.HasErrors(ds) {
		t.Fatalf("expected duplicate input name diagnostic, got %v", ds)
	}

	var found bool
	for _, d := range ds {
		if d.Severity == diag.SeverityError && strings.Contains(d.Message, "duplicate input name") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected diagnostic mentioning %q, got %v", "duplicate input name", ds)
	}
}

func TestBuild_MustModes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		src    string
		opts   di.Options
		wantOn bool
	}{
		{
			name: "no directive, cli off",
			src: `package test
type Container struct { X X ` + "`di:\"\"`" + ` }
type X struct{}
func NewX() X { return X{} }`,
			opts:   di.Options{Must: false},
			wantOn: false,
		},
		{
			name: "no directive, cli on",
			src: `package test
type Container struct { X X ` + "`di:\"\"`" + ` }
type X struct{}
func NewX() X { return X{} }`,
			opts:   di.Options{Must: true},
			wantOn: true,
		},
		{
			name: "directive must=true overrides cli off",
			src: `package test
//kanna:container must=true
type Container struct { X X ` + "`di:\"\"`" + ` }
type X struct{}
func NewX() X { return X{} }`,
			opts:   di.Options{Must: false},
			wantOn: true,
		},
		{
			name: "directive must=false overrides cli on",
			src: `package test
//kanna:container must=false
type Container struct { X X ` + "`di:\"\"`" + ` }
type X struct{}
func NewX() X { return X{} }`,
			opts:   di.Options{Must: true},
			wantOn: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p, _ := mustBuild(t, tt.src, "Container", tt.opts)
			if p.EmitMust != tt.wantOn {
				t.Errorf("EmitMust = %v, want %v", p.EmitMust, tt.wantOn)
			}
		})
	}
}

func TestBuild_NonBlankArgIsStored(t *testing.T) {
	t.Parallel()

	src := `package test
type DB struct{}
type Container struct {
	DB *DB ` + "`di:\"arg\"`" + `
}
`
	p, _ := mustBuild(t, src, "Container", di.Options{})

	if len(p.Inputs) != 1 || p.Inputs[0].Name != "db" {
		t.Fatalf("inputs = %+v, want one named db", p.Inputs)
	}
	if len(p.Outputs) != 1 || p.Outputs[0].FieldName != "DB" {
		t.Fatalf("outputs = %+v, want one for DB", p.Outputs)
	}
	if p.Steps[p.Outputs[0].StepIndex].Kind != di.StepKindInput {
		t.Errorf("expected stored arg output to point at an input step; got %+v",
			p.Steps[p.Outputs[0].StepIndex])
	}
}

func TestBuild_NonBlankArgWithCustomName(t *testing.T) {
	t.Parallel()

	src := `package test
type DB struct{}
type Container struct {
	DB *DB ` + "`di:\"arg=database\"`" + `
}
`
	p, _ := mustBuild(t, src, "Container", di.Options{})
	if p.Inputs[0].Name != "database" {
		t.Errorf("input name = %q, want database", p.Inputs[0].Name)
	}
	if p.Outputs[0].FieldName != "DB" {
		t.Errorf("output field = %q, want DB", p.Outputs[0].FieldName)
	}
}

func TestBuild_EmbedExposesFields(t *testing.T) {
	t.Parallel()

	src := `package test
type DB struct{}
type Repo struct{}
type Infra struct {
	DB *DB
}
func NewRepo(db *DB) *Repo { return nil }
type Container struct {
	_    *Infra ` + "`di:\"embed\"`" + `
	Repo *Repo  ` + "`di:\"\"`" + `
}
`
	p, _ := mustBuild(t, src, "Container", di.Options{})

	if len(p.Inputs) != 1 || p.Inputs[0].Name != "infra" {
		t.Fatalf("inputs = %+v, want one named infra", p.Inputs)
	}

	var foundEmbed, foundProvider bool
	for _, s := range p.Steps {
		switch s.Kind {
		case di.StepKindEmbedField:
			foundEmbed = true
			if s.EmbedFieldName != "DB" {
				t.Errorf("embed field = %q, want DB", s.EmbedFieldName)
			}
			if s.InputIndex != 0 {
				t.Errorf("embed input index = %d, want 0", s.InputIndex)
			}
		case di.StepKindProvider:
			foundProvider = true
		case di.StepKindInput:
			// not relevant for this assertion
		}
	}
	if !foundEmbed {
		t.Errorf("no StepKindEmbedField step emitted; steps=%+v", p.Steps)
	}
	if !foundProvider {
		t.Errorf("no provider step emitted")
	}
}

func TestBuild_EmbedPromotedField(t *testing.T) {
	t.Parallel()

	src := `package test
type DB struct{}
type Repo struct{}
type Common struct{ DB *DB }
type Infra struct{ Common }
func NewRepo(db *DB) *Repo { return nil }
type Container struct {
	_    *Infra ` + "`di:\"embed\"`" + `
	Repo *Repo  ` + "`di:\"\"`" + `
}
`
	p, _ := mustBuild(t, src, "Container", di.Options{})

	var embed di.Step
	for _, s := range p.Steps {
		if s.Kind == di.StepKindEmbedField {
			embed = s
			break
		}
	}
	if embed.EmbedFieldName == "" {
		t.Fatalf("no embed step found; steps=%+v", p.Steps)
	}
	if got, want := embed.EmbedFieldName, "Common.DB"; got != want {
		t.Errorf("embed field name = %q, want %q", got, want)
	}
	if embed.VarName != "db" {
		t.Errorf("embed var name = %q, want %q", embed.VarName, "db")
	}
}

func TestBuild_EmbedShallowerShadowsDeeper(t *testing.T) {
	t.Parallel()

	src := `package test
type DB struct{}
type Repo struct{}
type Common struct{ DB *DB }
type Infra struct {
	Common
	DB *DB
}
func NewRepo(db *DB) *Repo { return nil }
type Container struct {
	_    *Infra ` + "`di:\"embed\"`" + `
	Repo *Repo  ` + "`di:\"\"`" + `
}
`
	p, _ := mustBuild(t, src, "Container", di.Options{})

	for _, s := range p.Steps {
		if s.Kind != di.StepKindEmbedField {
			continue
		}
		if s.EmbedFieldName != "DB" {
			t.Errorf("embed field name = %q, want direct DB to shadow Common.DB", s.EmbedFieldName)
		}
	}
}

func TestBuild_EmbedAmbiguousAtSameDepth(t *testing.T) {
	t.Parallel()

	src := `package test
type DB struct{}
type Infra struct {
	DB1 *DB
	DB2 *DB
}
func NewRepo(db *DB) *DB { return nil }
type Container struct {
	_  *Infra ` + "`di:\"embed\"`" + `
	DB *DB   ` + "`di:\"with=NewRepo\"`" + `
}
`
	_, ds := build(t, src, "Container", di.Options{})
	if !diag.HasErrors(ds) {
		t.Fatalf("expected ambiguity error for two *DB fields at same depth, got %v", ds)
	}
}

func TestBuild_EmbedRejectsNonStruct(t *testing.T) {
	t.Parallel()

	src := `package test
type Greeter interface{ Greet() string }
type Container struct {
	_ Greeter ` + "`di:\"embed\"`" + `
}
`
	_, ds := build(t, src, "Container", di.Options{})
	if !diag.HasErrors(ds) {
		t.Fatalf("expected error diag, got %v", ds)
	}
}

func TestBuild_EmbedAmbiguousAcrossEmbeds(t *testing.T) {
	t.Parallel()

	src := `package test
type DB struct{}
type A struct { DB *DB }
type B struct { DB *DB }
type Container struct {
	_ *A ` + "`di:\"embed\"`" + `
	_ *B ` + "`di:\"embed\"`" + `
}
`
	_, ds := build(t, src, "Container", di.Options{})
	if !diag.HasErrors(ds) {
		t.Fatalf("expected error diag for duplicate embed field type, got %v", ds)
	}
}

func TestBuild_DirectArgWinsOverEmbed(t *testing.T) {
	t.Parallel()

	src := `package test
type DB struct{}
type Repo struct{}
type Infra struct { DB *DB }
func NewRepo(db *DB) *Repo { return nil }
type Container struct {
	_    *Infra ` + "`di:\"embed\"`" + `
	_    *DB    ` + "`di:\"arg\"`" + `
	Repo *Repo  ` + "`di:\"\"`" + `
}
`
	p, _ := mustBuild(t, src, "Container", di.Options{})

	for _, s := range p.Steps {
		if s.Kind == di.StepKindEmbedField {
			t.Errorf("did not expect any embed step when direct arg matches; got %+v", s)
		}
	}
}

func TestBuild_TypeAliasResolves(t *testing.T) {
	t.Parallel()

	src := `package test
type Real struct{}
type Alias = Real
func NewAlias() Alias { return Alias{} }
type Container struct {
	Field Alias ` + "`di:\"\"`" + `
}
`
	p, _ := mustBuild(t, src, "Container", di.Options{})
	if len(p.Steps) != 1 {
		t.Fatalf("steps = %d, want 1", len(p.Steps))
	}
	if p.Steps[0].Provider == nil || p.Steps[0].Provider.FuncName != "NewAlias" {
		t.Errorf("step[0] = %+v, want NewAlias", p.Steps[0])
	}
}

// build runs the whole scan then derive then plan pipeline on src and returns
// the plan plus the diagnostics Build produced.
func build(t *testing.T, src, containerName string, opts di.Options) (di.Plan, []diag.Diag) {
	t.Helper()

	pkg := pkgtest.LoadFile(t, src)
	pkgs := []*packages.Package{pkg}

	structs, dsS := scan.Structs(pkgs)
	if diag.HasErrors(dsS) {
		t.Fatalf("scan.Structs diags: %s", diag.Format(dsS))
	}
	cs, dsC := di.Containers(pkg.Fset, structs)
	if diag.HasErrors(dsC) {
		t.Fatalf("di.Containers diags: %s", diag.Format(dsC))
	}
	ps, dsP := di.Providers(pkgs)
	if diag.HasErrors(dsP) {
		t.Fatalf("di.Providers diags: %s", diag.Format(dsP))
	}

	var target di.Container
	for _, c := range cs {
		if c.StructName == containerName {
			target = c
			break
		}
	}
	if target.StructName == "" {
		t.Fatalf("container %q not found in scan results", containerName)
	}

	return di.Build(target, di.NewIndex(ps), opts)
}

// mustBuild is like build but fails the test if any error diagnostic is
// produced.
func mustBuild(t *testing.T, src, containerName string, opts di.Options) (di.Plan, []diag.Diag) {
	t.Helper()

	pl, ds := build(t, src, containerName, opts)
	if diag.HasErrors(ds) {
		t.Fatalf("Build returned errors: %s", diag.Format(ds))
	}
	return pl, ds
}
