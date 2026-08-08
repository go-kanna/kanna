package fixture_test

import (
	"go/token"
	"go/types"
	"reflect"
	"testing"

	"github.com/go-kanna/kanna/internal/gen/fixture"
	"github.com/go-kanna/kanna/internal/ir"
)

const pkgPath = "example.com/app/model"

var modelPkg = types.NewPackage(pkgPath, "model")

func namedStruct(pkg *types.Package, name string) *types.Named {
	obj := types.NewTypeName(token.NoPos, pkg, name, nil)

	return types.NewNamed(obj, types.NewStruct(nil, nil), nil)
}

func namedBasic(pkg *types.Package, name string, underlying types.Type) *types.Named {
	obj := types.NewTypeName(token.NoPos, pkg, name, nil)

	return types.NewNamed(obj, underlying, nil)
}

var (
	timeTime = namedStruct(types.NewPackage("time", "time"), "Time")
	uuidUUID = namedBasic(
		types.NewPackage("github.com/google/uuid", "uuid"),
		"UUID",
		types.NewArray(types.Typ[types.Byte], 16),
	)
)

const uuidExpr = "uuid.MustParse(gofakeit.UUID())"

func TestTagExpr(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		tag  string
		typ  types.Type
		want string
	}{
		{name: "skip", tag: "skip", typ: types.Typ[types.String], want: ""},
		{name: "dash skip", tag: "-", typ: types.Typ[types.String], want: ""},
		{name: "empty", tag: "", typ: types.Typ[types.String], want: ""},
		{name: "known template", tag: "{email}", typ: types.Typ[types.String], want: "gofakeit.Email()"},
		{name: "known template type mismatch", tag: "{email}", typ: types.Typ[types.Int], want: ""},
		{name: "date template on time", tag: "{date}", typ: timeTime, want: "gofakeit.Date()"},
		{name: "date template on string", tag: "{date}", typ: types.Typ[types.String], want: `mustGenerate("{date}")`},
		{name: "uuid template on uuid", tag: "{uuid}", typ: uuidUUID, want: uuidExpr},
		{name: "uuid template on string", tag: "{uuid}", typ: types.Typ[types.String], want: "gofakeit.UUID()"},
		{name: "date template on uuid", tag: "{date}", typ: uuidUUID, want: ""},
		{
			name: "unknown template on string",
			tag:  "###-####",
			typ:  types.Typ[types.String],
			want: `mustGenerate("###-####")`,
		},
		{name: "unknown template on int", tag: "{foo:1,10}", typ: types.Typ[types.Int], want: ""},
		{name: "number on int", tag: "{number:1,10}", typ: types.Typ[types.Int], want: "gofakeit.Number(1, 10)"},
		{
			name: "number on int64 converts",
			tag:  "{number:1,10}",
			typ:  types.Typ[types.Int64],
			want: "int64(gofakeit.Number(1, 10))",
		},
		{
			name: "number on string falls back to generate",
			tag:  "{number:1,10}",
			typ:  types.Typ[types.String],
			want: `mustGenerate("{number:1,10}")`,
		},
		{name: "number with spaces", tag: "{number: 1, 10}", typ: types.Typ[types.Int], want: "gofakeit.Number(1, 10)"},
		{name: "number wrong arity", tag: "{number:1}", typ: types.Typ[types.Int], want: ""},
		{name: "number with bad args", tag: "{number:a,b}", typ: types.Typ[types.Int], want: ""},
		{name: "negative int arg", tag: "{intrange:-5,5}", typ: types.Typ[types.Int], want: "gofakeit.IntRange(-5, 5)"},
		{name: "uintrange on uint", tag: "{uintrange:1,10}", typ: types.Typ[types.Uint], want: "gofakeit.UintRange(1, 10)"},
		{name: "negative uint arg", tag: "{uintrange:-1,10}", typ: types.Typ[types.Uint], want: ""},
		{
			name: "price on float64",
			tag:  "{price:1.00,10.00}",
			typ:  types.Typ[types.Float64],
			want: "gofakeit.Price(1.00, 10.00)",
		},
		{
			name: "float32range on float32",
			tag:  "{float32range:0.5,2.5}",
			typ:  types.Typ[types.Float32],
			want: "gofakeit.Float32Range(0.5, 2.5)",
		},
		{name: "inf float arg", tag: "{price:Inf,10}", typ: types.Typ[types.Float64], want: ""},
		{name: "negative range on unsigned field", tag: "{intrange:-5,5}", typ: types.Typ[types.Uint8], want: ""},
		{
			name: "number fitting a narrow field converts",
			tag:  "{number:1,10}",
			typ:  types.Typ[types.Uint8],
			want: "uint8(gofakeit.Number(1, 10))",
		},
		{name: "number overflowing a narrow field", tag: "{number:1,300}", typ: types.Typ[types.Int8], want: ""},
		{
			name: "number overflowing 32-bit int stays zero for portability",
			tag:  "{number:1,3000000000}",
			typ:  types.Typ[types.Int64],
			want: "",
		},
		{
			name: "float32range overflowing float32",
			tag:  "{float32range:1e39,2e39}",
			typ:  types.Typ[types.Float32],
			want: "",
		},
		{name: "float args on an int field", tag: "{price:1.5,2.5}", typ: types.Typ[types.Int], want: ""},
		{name: "sentence on string", tag: "{sentence:5}", typ: types.Typ[types.String], want: "gofakeit.Sentence(5)"},
		{name: "sentence on int does not convert", tag: "{sentence:5}", typ: types.Typ[types.Int], want: ""},
		{
			name: "number on named int converts through the named type",
			tag:  "{number:1,10}",
			typ:  namedBasic(modelPkg, "Level", types.Typ[types.Int]),
			want: "model.Level(gofakeit.Number(1, 10))",
		},
		{
			name: "word on named string converts through the named type",
			tag:  "{word}",
			typ:  namedBasic(modelPkg, "Status", types.Typ[types.String]),
			want: "model.Status(gofakeit.Word())",
		},
		{
			name: "unknown template on named string converts the fallback",
			tag:  "###",
			typ:  namedBasic(modelPkg, "Code", types.Typ[types.String]),
			want: `model.Code(mustGenerate("###"))`,
		},
		{
			name: "named string from another package stays zero",
			tag:  "{word}",
			typ:  namedBasic(types.NewPackage("example.com/other", "other"), "Status", types.Typ[types.String]),
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := fixture.TagExpr(tt.tag, tt.typ, pkgPath, "model"); got != tt.want {
				t.Errorf("tagExpr(%q) = %q, want %q", tt.tag, got, tt.want)
			}
		})
	}
}

func TestNameExpr(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		fieldName string
		typ       types.Type
		want      string
		wantOK    bool
	}{
		{name: "email", fieldName: "Email", typ: types.Typ[types.String], want: "gofakeit.Email()", wantOK: true},
		{name: "email type mismatch", fieldName: "Email", typ: types.Typ[types.Int], wantOK: false},
		{name: "name", fieldName: "Name", typ: types.Typ[types.String], want: "gofakeit.Name()", wantOK: true},
		{
			name:      "address",
			fieldName: "Address",
			typ:       types.Typ[types.String],
			want:      "gofakeit.Address().Address",
			wantOK:    true,
		},
		{name: "created at", fieldName: "CreatedAt", typ: timeTime, want: "gofakeit.Date()", wantOK: true},
		{name: "at suffix type mismatch", fieldName: "CreatedAt", typ: types.Typ[types.String], wantOK: false},
		{name: "unknown", fieldName: "Score", typ: types.Typ[types.String], wantOK: false},
		{
			name:      "named string is not a heuristic match",
			fieldName: "Name",
			typ:       namedBasic(modelPkg, "Username", types.Typ[types.String]),
			wantOK:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := fixture.NameExpr(tt.fieldName, tt.typ)
			if ok != tt.wantOK || got != tt.want {
				t.Errorf("nameExpr(%q) = %q, %t, want %q, %t", tt.fieldName, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestTypeExpr(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		typ  types.Type
		want string
	}{
		{name: "string", typ: types.Typ[types.String], want: "gofakeit.Word()"},
		{name: "bool", typ: types.Typ[types.Bool], want: "gofakeit.Bool()"},
		{name: "int", typ: types.Typ[types.Int], want: "gofakeit.Int()"},
		{name: "int64", typ: types.Typ[types.Int64], want: "gofakeit.Int64()"},
		{name: "uint8", typ: types.Typ[types.Uint8], want: "gofakeit.Uint8()"},
		{name: "float64", typ: types.Typ[types.Float64], want: "gofakeit.Float64()"},
		{name: "time", typ: timeTime, want: "gofakeit.Date()"},
		{name: "uuid", typ: uuidUUID, want: uuidExpr},
		{name: "pointer to uuid", typ: types.NewPointer(uuidUUID), want: ""},
		{name: "named string", typ: namedBasic(modelPkg, "Status", types.Typ[types.String]), want: ""},
		{name: "pointer", typ: types.NewPointer(types.Typ[types.String]), want: ""},
		{name: "slice", typ: types.NewSlice(types.Typ[types.String]), want: ""},
		{name: "map", typ: types.NewMap(types.Typ[types.String], types.Typ[types.String]), want: ""},
		{name: "chan", typ: types.NewChan(types.SendRecv, types.Typ[types.Int]), want: ""},
		{name: "uintptr", typ: types.Typ[types.Uintptr], want: ""},
		{name: "complex128", typ: types.Typ[types.Complex128], want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := fixture.TypeExpr(tt.typ); got != tt.want {
				t.Errorf("typeExpr(%s) = %q, want %q", tt.typ, got, tt.want)
			}
		})
	}
}

func TestPlans(t *testing.T) {
	t.Parallel()

	profile := namedStruct(modelPkg, "Profile")
	selfRef := namedStruct(modelPkg, "Node")
	cycleA := namedStruct(modelPkg, "CycleA")
	cycleB := namedStruct(modelPkg, "CycleB")
	excluded := namedStruct(modelPkg, "Excluded")

	targets := []fixture.Target{
		{Name: "CycleA", Fields: []ir.Field{
			{Name: "B", Type: cycleB},
		}},
		{Name: "CycleB", Fields: []ir.Field{
			{Name: "A", Type: cycleA},
		}},
		{Name: "Node", Fields: []ir.Field{
			{Name: "Next", Type: selfRef},
			{Name: "Label", Type: types.Typ[types.String]},
		}},
		{Name: "Profile", Fields: []ir.Field{
			{Name: "Bio", Type: types.Typ[types.String]},
		}},
		{Name: "User", Fields: []ir.Field{
			{Name: "ID", Type: types.Typ[types.Int64]},
			{Name: "Token", Type: uuidUUID},
			{Name: "Name", Type: types.Typ[types.String], Tag: `fake:"{firstname}"`},
			{Name: "Memo", Type: types.Typ[types.String], Tag: `fake:""`},
			{Name: "CreatedAt", Type: timeTime},
			{Name: "Profile", Type: profile},
			{Name: "Ex", Type: excluded},
			{Name: "Note", Type: types.NewPointer(types.Typ[types.String])},
		}},
	}

	want := []fixture.Plan{
		{Name: "CycleA"},
		{Name: "CycleB"},
		{Name: "Node", Fields: []fixture.Assignment{
			{Name: "Label", Expr: "gofakeit.Word()"},
		}},
		{Name: "Profile", Fields: []fixture.Assignment{
			{Name: "Bio", Expr: "gofakeit.Word()"},
		}},
		{Name: "User", Fields: []fixture.Assignment{
			{Name: "ID", Expr: "gofakeit.Int64()"},
			{Name: "Token", Expr: uuidExpr},
			{Name: "Name", Expr: "gofakeit.FirstName()"},
			{Name: "Memo", Expr: "gofakeit.Word()"},
			{Name: "CreatedAt", Expr: "gofakeit.Date()"},
			{Name: "Profile", Expr: "Profile()"},
		}},
	}

	got, imports := fixture.Plans(targets, pkgPath, "model")
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Plans() = %+v, want %+v", got, want)
	}

	wantImports := []string{"github.com/google/uuid"}
	if !reflect.DeepEqual(imports, wantImports) {
		t.Errorf("Plans() imports = %v, want %v", imports, wantImports)
	}
}
