package di

import (
	"go/token"

	"github.com/go-kanna/kanna/internal/diag"
	"github.com/go-kanna/kanna/internal/ir"
	"github.com/go-kanna/kanna/internal/scan"
)

// tagKey is the struct tag key that marks a field for injection. Each kanna
// generator claims a short tag key of its own, so they compose on one field the
// way json and db tags do.
const tagKey = "di"

// Containers derives the DI containers from the structs reported by scan.
//
// A struct becomes a container when at least one of its fields carries a di tag.
// A struct with no di tag at all is skipped without a diagnostic, since scan
// reports every struct in the package regardless of relevance — unless it is
// annotated with a //kanna:container directive, which makes the missing tag a
// mistake worth reporting.
//
// fset is the file set the structs were parsed with. It is needed to evaluate a
// directive's returns= expression in the scope of the declaring file.
func Containers(fset *token.FileSet, structs []ir.Struct) ([]Container, []diag.Diag) {
	var (
		containers []Container
		diags      []diag.Diag
	)

	for _, s := range structs {
		c, ds, ok := containerOf(fset, s)
		diags = append(diags, ds...)
		if ok {
			containers = append(containers, c)
		}
	}

	return containers, diags
}

// containerOf converts s into a Container. The final return value reports
// whether s is a container at all; diagnostics are returned either way so that a
// malformed tag is reported even when it leaves the struct with no usable
// fields.
func containerOf(fset *token.FileSet, s ir.Struct) (Container, []diag.Diag, bool) {
	fields, diags := containerFields(s)

	pd, errs := ParseDirective(s.Doc)
	for _, msg := range errs {
		diags = append(diags, diag.Errorf(s.Pos, "%s", msg))
	}

	if len(fields) == 0 {
		// Silence is right for a struct that never asked to be a container, but
		// an explicit directive means the author expected one.
		if pd.Found {
			diags = append(diags, diag.Errorf(s.Pos,
				"//kanna:container on %s but no field carries a %s tag", s.Name, tagKey))
		}
		return Container{}, diags, false
	}

	// A generic container is rejected here rather than later: the emitted
	// constructor would carry the type parameters into its signature and fail to
	// parse, and because a package is emitted as one file that failure would
	// take every sibling container's output down with it.
	if s.Named != nil && s.Named.TypeParams().Len() > 0 {
		diags = append(diags, diag.Errorf(s.Pos,
			"generic container %s is not supported: a constructor has no way to choose its type arguments", s.Name))
		return Container{}, diags, false
	}

	directive, dds := buildDirective(fset, s, pd)
	diags = append(diags, dds...)

	return Container{
		PkgPath:    s.PkgPath,
		PkgName:    s.PkgName,
		StructName: s.Name,
		Named:      s.Named,
		Pos:        s.Pos,
		Directive:  directive,
		Fields:     fields,
	}, diags, true
}

// containerFields returns a Field for every field of s that carries a di tag.
// Fields without the tag are ignored.
func containerFields(s ir.Struct) ([]Field, []diag.Diag) {
	var (
		fields []Field
		diags  []diag.Diag
	)

	for _, f := range s.Fields {
		value, ok := f.Tag.Lookup(tagKey)
		if !ok {
			continue
		}

		parsed, err := ParseTag(value)
		if err != nil {
			diags = append(diags, diag.Errorf(f.Pos, "%s", err))
			continue
		}

		if f.Embedded {
			diags = append(diags, diag.Errorf(f.Pos, "embedded field with %s tag is not supported", tagKey))
			continue
		}

		role, d := decideRole(f.Name, parsed, f.Pos)
		if d != nil {
			diags = append(diags, *d)
			continue
		}

		fields = append(fields, Field{
			Name:        f.Name,
			Type:        f.Type,
			Role:        role,
			ProviderRef: ProviderRef{Raw: parsed.With},
			ArgName:     parsed.ArgName,
			IsReturns:   parsed.Kind == TagReturns,
			Pos:         f.Pos,
		})
	}

	return fields, diags
}

// decideRole maps a (field name, parsed tag) pair onto a Role, returning a
// diagnostic when the combination is not legal.
func decideRole(fieldName string, pt ParsedTag, pos token.Position) (Role, *diag.Diag) {
	blank := fieldName == "_"

	switch pt.Kind {
	case TagMarker:
		if blank {
			d := diag.Errorf(pos,
				`_ field requires di:"with=...", di:"arg", di:"returns" or di:"embed"`)
			return 0, &d
		}
		return RoleOut, nil

	case TagWith:
		if blank {
			return RoleOverride, nil
		}
		return RoleOut, nil

	case TagArg:
		// Blank: constructor input only. Non-blank: constructor input whose
		// value is also stored in the named container field.
		return RoleArg, nil

	case TagReturns:
		if blank {
			return RoleReturnsOnly, nil
		}
		return RoleOut, nil

	case TagEmbed:
		if !blank {
			d := diag.Errorf(pos, `di:"embed" requires a blank field (_)`)
			return 0, &d
		}
		return RoleEmbed, nil

	case TagInvalid:
		fallthrough
	default:
		d := diag.Errorf(pos, "internal: unrecognized di tag form")
		return 0, &d
	}
}

// buildDirective converts a ParsedDirective into a Directive, resolving the
// returns expression to a concrete type when one is present.
//
// The expression is evaluated at the position of the struct declaration so that
// the file's imports are in scope, which is what lets a qualified name such as
// greeter.Greeter resolve.
func buildDirective(fset *token.FileSet, s ir.Struct, pd ParsedDirective) (Directive, []diag.Diag) {
	d := Directive{
		Name: pd.Name,
		Must: pd.Must,
	}
	if pd.ReturnsExpr == "" {
		return d, nil
	}

	obj := s.Named.Obj()
	t, err := scan.ResolveTypeExpr(fset, obj.Pkg(), obj.Pos(), pd.ReturnsExpr)
	if err != nil {
		return d, []diag.Diag{
			diag.Errorf(s.Pos, "directive returns=%s: %v", pd.ReturnsExpr, err),
		}
	}
	d.ReturnType = t

	return d, nil
}
