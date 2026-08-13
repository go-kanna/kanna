package relation_test

import (
	"go/token"
	"go/types"
	"testing"

	"github.com/go-kanna/kanna/internal/relation"
)

// namedStruct builds a named struct type in a package of its own, which is
// all the shape helpers look at.
func namedStruct(pkgPath, pkgName, name string) *types.Named {
	pkg := types.NewPackage(pkgPath, pkgName)
	return types.NewNamed(types.NewTypeName(token.NoPos, pkg, name, nil), types.NewStruct(nil, nil), nil)
}

// withScanMethod attaches the exact sql.Scanner contract — Scan(any) error —
// to the named type, with a pointer receiver.
func withScanMethod(named *types.Named) *types.Named {
	pkg := named.Obj().Pkg()
	recv := types.NewVar(token.NoPos, pkg, "", types.NewPointer(named))
	sig := types.NewSignatureType(recv, nil, nil,
		types.NewTuple(types.NewVar(token.NoPos, pkg, "src", types.Universe.Lookup("any").Type())),
		types.NewTuple(types.NewVar(token.NoPos, pkg, "", types.Universe.Lookup("error").Type())),
		false)
	named.AddMethod(types.NewFunc(token.NoPos, pkg, "Scan", sig))
	return named
}

func TestSliceElem(t *testing.T) {
	t.Parallel()

	user := namedStruct("example.com/m", "m", "User")
	if elem, ok := relation.SliceElem(types.NewSlice(user)); !ok || elem != user {
		t.Errorf("SliceElem([]User) = %v, %v; want User, true", elem, ok)
	}
	if _, ok := relation.SliceElem(user); ok {
		t.Error("SliceElem(User) reported a slice")
	}
}

func TestPointerElem(t *testing.T) {
	t.Parallel()

	user := namedStruct("example.com/m", "m", "User")
	if elem, ok := relation.PointerElem(types.NewPointer(user)); !ok || elem != user {
		t.Errorf("PointerElem(*User) = %v, %v; want User, true", elem, ok)
	}
	if _, ok := relation.PointerElem(user); ok {
		t.Error("PointerElem(User) reported a pointer")
	}
}

func TestStructNamed(t *testing.T) {
	t.Parallel()

	user := namedStruct("example.com/m", "m", "User")
	if named, ok := relation.StructNamed(user); !ok || named != user {
		t.Errorf("StructNamed(User) = %v, %v; want User, true", named, ok)
	}
	if _, ok := relation.StructNamed(types.Typ[types.Int]); ok {
		t.Error("StructNamed(int) reported a struct")
	}

	pkg := types.NewPackage("example.com/m", "m")
	namedInt := types.NewNamed(types.NewTypeName(token.NoPos, pkg, "Key", nil), types.Typ[types.Int64], nil)
	if _, ok := relation.StructNamed(namedInt); ok {
		t.Error("StructNamed(named int64) reported a struct")
	}
}

func TestRelationShape(t *testing.T) {
	t.Parallel()

	user := namedStruct("example.com/m", "m", "User")

	tests := []struct {
		name string
		typ  types.Type
		want bool
	}{
		{"slice of structs", types.NewSlice(user), true},
		{"pointer to struct", types.NewPointer(user), true},
		{"bare struct", user, false},
		{"slice of basics", types.NewSlice(types.Typ[types.String]), false},
		{"basic", types.Typ[types.Int], false},
		// Struct types that still read as columns stay columns.
		{"pointer to time.Time", types.NewPointer(namedStruct("time", "time", "Time")), false},
		{"pointer to a Scanner", types.NewPointer(withScanMethod(namedStruct("example.com/m", "m", "NullThing"))), false},
		{"slice of Scanners", types.NewSlice(withScanMethod(namedStruct("example.com/m", "m", "RawRow"))), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := relation.RelationShape(tt.typ); got != tt.want {
				t.Errorf("RelationShape(%s) = %v, want %v", tt.typ, got, tt.want)
			}
		})
	}
}
