// Package di generates dependency-injection constructors from container struct
// declarations.
//
// The types in this file are the DI-specific interpretation of what the shared
// scan layer reports: a container is a struct whose fields carry di tags,
// and a provider is a top-level function that can produce a value. Both are
// derived from ir.Struct rather than discovered by scanning directly.
package di

import (
	"go/token"
	"go/types"
)

// Container is a struct that di generates a constructor for.
//
// A struct becomes a container when at least one of its fields carries an
// di:"..." tag. Metadata such as the constructor name, return type, and
// must mode comes from an optional //kanna:container directive.
type Container struct {
	PkgPath    string
	PkgName    string
	StructName string

	// Named is the container's declared type. Its underlying type is always
	// *types.Struct.
	Named *types.Named

	Pos       token.Position
	Directive Directive
	Fields    []Field
}

// Field is a field of a container struct that participates in dependency
// injection. Fields without an di tag are not represented here.
type Field struct {
	// Name is the field's identifier. "_" indicates a blank field that does
	// not participate in the struct literal.
	Name string

	// Type is the resolved Go type of the field.
	Type types.Type

	// Role describes how the field participates in resolution.
	Role Role

	// ProviderRef holds the textual provider reference declared via with=...
	// It is empty for auto-resolved fields (di:"") and for arg fields.
	ProviderRef ProviderRef

	// ArgName overrides the constructor argument name when Role is RoleArg.
	// Empty means derive a name from the field type.
	ArgName string

	// IsReturns marks this field as the source of the container's return type.
	// It applies to RoleOut (output + returns) and RoleReturnsOnly (returns only).
	IsReturns bool

	Pos token.Position
}

// Role describes the role a field plays in dependency resolution.
type Role int

const (
	// RoleOut is a field that becomes part of the constructed container value.
	// The field's value is supplied by a resolved provider.
	RoleOut Role = iota

	// RoleArg is a field that declares a constructor argument whose value is
	// passed in by the caller. A blank field only declares the argument, while
	// a named field also stores the value in the container.
	RoleArg

	// RoleOverride is a blank field that overrides which provider is used
	// for its declared type. The value is not stored in the container.
	RoleOverride

	// RoleReturnsOnly is a blank field that declares the container's return
	// type but otherwise contributes nothing to construction.
	RoleReturnsOnly

	// RoleEmbed is a blank field whose type is a struct (or pointer to a
	// struct) passed in as a constructor argument and whose exported fields
	// are usable as resolution sources for the containing container.
	RoleEmbed
)

// Provider is a top-level function that can produce a value used for dependency
// injection. Providers return either (T) or (T, error).
type Provider struct {
	PkgPath      string
	PkgName      string
	FuncName     string
	Result       types.Type
	Params       []types.Type
	ReturnsError bool

	// Generated reports whether the function was declared in a file carrying the
	// generated code marker, meaning an earlier run emitted it. Such a provider
	// is a legitimate candidate — downstream containers resolve against emitted
	// constructors on purpose — but when it makes a resolution ambiguous the
	// diagnostic needs to say where it came from.
	Generated bool

	Pos token.Position
}

// Directive captures metadata supplied by a //kanna:container comment.
//
// All fields are optional. Empty/zero values mean "fall back to defaults"
// (constructor name derived from struct name, return type *<Container>, no
// MustNew constructor). The CLI may supply defaults during planning.
type Directive struct {
	// Name overrides the constructor name. Empty means derive from struct name.
	Name string

	// ReturnType overrides the container's return type. Nil means *<Container>.
	ReturnType types.Type

	// Must indicates whether MustNew* should be generated for this container.
	// The MustUnset state allows CLI defaults to apply.
	Must MustMode
}

// MustMode controls generation of MustNew* constructors.
type MustMode int

const (
	// MustUnset means the directive did not specify a value. CLI default applies.
	MustUnset MustMode = iota

	// MustOff explicitly disables MustNew* generation for this container.
	MustOff

	// MustOn enables MustNew* generation (panic on error).
	MustOn
)

// ProviderRef refers to a specific provider by its textual name as written by
// the user. Resolution to a concrete provider happens during planning.
type ProviderRef struct {
	// Raw is the reference as written, for example "config.NewWriterDB" or
	// simply "NewWriterDB". An empty Raw means no reference was given.
	Raw string
}

// HasRef reports whether a textual provider reference is present.
func (r ProviderRef) HasRef() bool {
	return r.Raw != ""
}
