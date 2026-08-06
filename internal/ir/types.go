// Package ir defines the generator-agnostic model that the scan layer produces
// and each generator consumes. Types in this package carry no behavior: they
// exist as boundary types between scan and the per-generator planning that
// interprets them.
package ir

import (
	"go/token"
	"go/types"
	"reflect"
)

// Struct is a struct type declared in a scanned package.
//
// Scan reports every struct it finds and applies no filtering, because the
// generators disagree on what is relevant: some opt in via a struct tag while
// others take a whole package and exclude by directive. Callers therefore
// decide how to treat unexported types, generic types, and unexported fields.
type Struct struct {
	// PkgPath is the import path of the declaring package.
	PkgPath string

	// PkgName is the name of the declaring package.
	PkgName string

	// Name is the type's identifier as declared.
	Name string

	// Named is the declared type. Its underlying type is always *types.Struct.
	// Use Named.TypeParams() to detect a generic declaration.
	Named *types.Named

	// Pos is the position of the type name.
	Pos token.Position

	// Doc holds the raw comment lines attached to the declaration, each still
	// carrying its leading "//". Directive syntax differs per generator, so
	// scan collects the lines without interpreting them.
	Doc []string

	// Fields holds every field in declaration order, including unexported and
	// embedded ones.
	Fields []Field
}

// Field is a single field of a Struct.
type Field struct {
	// Name is the field identifier. For an embedded field it is the name of the
	// embedded type.
	Name string

	// Type is the resolved type of the field.
	Type types.Type

	// Tag is the field's struct tag. Each generator looks up its own keys.
	Tag reflect.StructTag

	// Pos is the position of the field declaration.
	Pos token.Position

	// Exported reports whether the field name is exported.
	Exported bool

	// Embedded reports whether the field was declared without a name.
	Embedded bool
}
