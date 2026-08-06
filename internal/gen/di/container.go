package di

import (
	"go/token"

	"github.com/go-kanna/kanna/internal/diag"
	"github.com/go-kanna/kanna/internal/ir"
	"github.com/go-kanna/kanna/internal/packages"
	"github.com/go-kanna/kanna/internal/scan"
)

// tagKey is the struct tag key that marks a field for injection. Each kanna
// generator claims a short tag key of its own, so they compose on one field the
// way json and db tags do.
const tagKey = "di"

// Containers derives the DI containers from the structs reported by scan.
//
// A struct becomes a container when at least one of its fields carries an
// di tag; structs without any are skipped without a diagnostic, since the
// scan layer reports every struct in the package regardless of relevance.
//
// pkgs supplies the type-checked packages the structs came from. They are needed
// to resolve a directive's returns= expression in the declaring package's scope,
// which is matched by import path.
func Containers(pkgs []*packages.Package, structs []ir.Struct) ([]Container, []diag.Diag) {
	byPath := make(map[string]*packages.Package, len(pkgs))
	for _, pkg := range pkgs {
		if pkg != nil {
			byPath[pkg.PkgPath] = pkg
		}
	}

	var (
		containers []Container
		diags      []diag.Diag
	)

	for _, s := range structs {
		c, ds, ok := containerOf(byPath[s.PkgPath], s)
		diags = append(diags, ds...)
		if ok {
			containers = append(containers, c)
		}
	}

	return containers, diags
}

// containerOf converts s into a Container. The final return value reports
// whether s is a container at all; diagnostics are returned either way so that
// a malformed tag is reported even when it leaves the struct with no usable
// fields.
func containerOf(pkg *packages.Package, s ir.Struct) (Container, []diag.Diag, bool) {
	fields, diags := containerFields(s)
	if len(fields) == 0 {
		return Container{}, diags, false
	}

	pd, errs := ParseDirective(s.Doc)
	for _, msg := range errs {
		diags = append(diags, diag.Errorf(s.Pos, "%s", msg))
	}

	directive, dds := buildDirective(pkg, pd, s.Pos)
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
			diags = append(diags, diag.Errorf(f.Pos, "embedded field with di tag is not supported"))
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
			d := diag.Errorf(pos, `_ field requires di:"with=...", di:"arg" or di:"returns"`)
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
func buildDirective(pkg *packages.Package, pd ParsedDirective, pos token.Position) (Directive, []diag.Diag) {
	d := Directive{
		Name: pd.Name,
		Must: pd.Must,
	}
	if pd.ReturnsExpr == "" {
		return d, nil
	}
	if pkg == nil {
		return d, []diag.Diag{
			diag.Errorf(pos, "directive returns=%s: declaring package is unavailable", pd.ReturnsExpr),
		}
	}

	t, err := scan.ResolveTypeExpr(pkg, pd.ReturnsExpr)
	if err != nil {
		return d, []diag.Diag{
			diag.Errorf(pos, "directive returns=%s: %v", pd.ReturnsExpr, err),
		}
	}
	d.ReturnType = t

	return d, nil
}
