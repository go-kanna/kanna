package di

import (
	"go/token"
	"go/types"
	"strings"

	"github.com/go-kanna/kanna/internal/diag"
)

func constructorNameFor(c Container) string {
	if c.Directive.Name != "" {
		return c.Directive.Name
	}
	return "New" + upperFirst(c.StructName)
}

func resolveReturnType(c Container) (types.Type, *diag.Diag) {
	var taggedReturns *Field
	for i := range c.Fields {
		f := &c.Fields[i]
		if !f.IsReturns {
			continue
		}
		if taggedReturns != nil {
			d := diag.Errorf(f.Pos,
				`multiple di:"returns" fields (also at %s)`, taggedReturns.Pos)
			return nil, &d
		}
		taggedReturns = f
	}

	if c.Directive.ReturnType != nil {
		if taggedReturns != nil {
			d := diag.Errorf(c.Pos,
				`directive returns= conflicts with di:"returns" on field %s`,
				taggedReturns.Name)
			return nil, &d
		}
		return c.Directive.ReturnType, nil
	}

	if taggedReturns != nil {
		return taggedReturns.Type, nil
	}
	if c.Named != nil {
		return types.NewPointer(c.Named), nil
	}
	return nil, nil
}

func mergeMust(d MustMode, cliMust bool) bool {
	switch d {
	case MustOn:
		return true
	case MustOff:
		return false
	case MustUnset:
		fallthrough
	default:
		return cliMust
	}
}

func buildInputs(c Container) ([]Input, []diag.Diag) {
	var (
		inputs []Input
		diags  []diag.Diag
	)
	seenTypes := map[string]token.Position{}
	seenNames := map[string]token.Position{}

	for _, f := range c.Fields {
		if f.Role != RoleArg && f.Role != RoleEmbed {
			continue
		}
		name := f.ArgName
		if name == "" {
			name = deriveInputName(f.Type)
		}
		tk := TypeKey(f.Type)
		if prev, ok := seenTypes[tk]; ok {
			diags = append(diags, diag.Errorf(f.Pos,
				"duplicate input type %s (first declared at %s)", TypeString(f.Type), prev))
			continue
		}
		if prev, ok := seenNames[name]; ok {
			diags = append(diags, diag.Errorf(f.Pos,
				`duplicate input name %q (first declared at %s); use di:"arg=..." to disambiguate`,
				name, prev))
			continue
		}
		seenTypes[tk] = f.Pos
		seenNames[name] = f.Pos
		inputs = append(inputs, Input{Name: name, Type: f.Type})
	}
	return inputs, diags
}

// buildEmbeds walks the container's RoleEmbed fields and returns a TypeKey to
// embedSource map of exported sub-fields available as resolution sources.
//
// Each embed input must be a struct (or pointer to a struct); other shapes
// produce diagnostics. Promoted fields reached through anonymous embeds are also
// exposed; shallower fields shadow deeper ones inside a single embed (matching
// Go's selector semantics), while equal-depth duplicates within one embed and
// same-type sources across two embeds are both reported as errors.
func buildEmbeds(c Container, inputs []Input) (map[string]embedSource, []diag.Diag) {
	out := map[string]embedSource{}
	var diags []diag.Diag

	indexByType := make(map[string]int, len(inputs))
	for i, in := range inputs {
		indexByType[TypeKey(in.Type)] = i
	}

	for _, f := range c.Fields {
		if f.Role != RoleEmbed {
			continue
		}
		idx, ok := indexByType[TypeKey(f.Type)]
		if !ok {
			// The corresponding input was rejected (duplicate type/name).
			continue
		}
		st, ok := structOf(f.Type)
		if !ok {
			diags = append(diags, diag.Errorf(f.Pos,
				`di:"embed" requires a struct or pointer to struct, got %s`,
				TypeString(f.Type)))
			continue
		}
		sources, srcDiags := embedSourcesOf(f.Type, st, idx, f.Pos, inputs)
		diags = append(diags, srcDiags...)
		for tk, src := range sources {
			if existing, dup := out[tk]; dup {
				diags = append(diags, diag.Errorf(f.Pos,
					"embed: multiple sources for %s (also %s.%s)",
					TypeString(src.FieldType),
					inputs[existing.InputIndex].Name, existing.FieldName))
				continue
			}
			out[tk] = src
		}
	}
	return out, diags
}

// embedSourcesOf walks a single embed input breadth-first, recording each
// exported field (direct or promoted through anonymous embeds) keyed by TypeKey.
// The traversal mirrors Go's selector promotion: a shallower field wins over
// deeper ones of the same type, while same-depth duplicates are reported as
// ambiguity diagnostics and skipped.
func embedSourcesOf(
	rootType types.Type,
	rootSt *types.Struct,
	inputIdx int,
	fPos token.Position,
	inputs []Input,
) (map[string]embedSource, []diag.Diag) {
	out := map[string]embedSource{}
	claimed := map[string]bool{}
	var diags []diag.Diag

	type frame struct {
		st     *types.Struct
		prefix string
	}
	visited := map[string]bool{TypeKey(rootType): true}
	level := []frame{{rootSt, ""}}

	for len(level) > 0 {
		var next []frame
		levelCands := map[string][]embedSource{}

		for _, fr := range level {
			for sf := range fr.st.Fields() {
				if !sf.Exported() {
					continue
				}
				name := sf.Name()
				if fr.prefix != "" {
					name = fr.prefix + "." + name
				}
				tk := TypeKey(sf.Type())
				if !claimed[tk] {
					levelCands[tk] = append(levelCands[tk], embedSource{
						InputIndex: inputIdx,
						FieldName:  name,
						FieldType:  sf.Type(),
					})
				}
				if sf.Anonymous() && !visited[tk] {
					visited[tk] = true
					if subst, ok := structOf(sf.Type()); ok {
						next = append(next, frame{subst, name})
					}
				}
			}
		}

		for tk, cands := range levelCands {
			if len(cands) > 1 {
				names := make([]string, 0, len(cands))
				for _, c := range cands {
					names = append(names, inputs[c.InputIndex].Name+"."+c.FieldName)
				}
				diags = append(diags, diag.Errorf(fPos,
					"embed: ambiguous source for %s at the same depth (%s)",
					TypeString(cands[0].FieldType), strings.Join(names, ", ")))
				claimed[tk] = true
				continue
			}
			out[tk] = cands[0]
			claimed[tk] = true
		}

		level = next
	}
	return out, diags
}

// structOf returns the underlying *types.Struct of t (unwrapping a leading
// pointer and resolving type aliases) and reports whether t had a struct shape
// at all.
func structOf(t types.Type) (*types.Struct, bool) {
	t = types.Unalias(t)
	if ptr, ok := t.(*types.Pointer); ok {
		t = types.Unalias(ptr.Elem())
	}
	if named, ok := t.(*types.Named); ok {
		if st, ok := named.Underlying().(*types.Struct); ok {
			return st, true
		}
		return nil, false
	}
	if st, ok := t.(*types.Struct); ok {
		return st, true
	}
	return nil, false
}

// buildOverrides walks fields whose di tag names a specific provider
// (`di:"with=..."`) and indexes them by their declared type. The resolver
// consults this map ahead of provider-by-type lookup, so that any transitive
// dependency inside the same container resolves to the same provider the user
// picked for the field.
//
// Both blank (RoleOverride) and non-blank (RoleOut with a ref) fields
// contribute. The non-blank case lets a stored field double as a container-wide
// override; users do not need a redundant blank twin to disambiguate sibling
// resolutions.
func buildOverrides(c Container, idx *Index) (map[string]*Provider, []diag.Diag) {
	out := map[string]*Provider{}
	posByType := map[string]token.Position{}
	var diags []diag.Diag

	for _, f := range c.Fields {
		if f.Role != RoleOverride && f.Role != RoleOut {
			continue
		}
		if !f.ProviderRef.HasRef() {
			continue
		}
		candidates := idx.LookupByRef(f.ProviderRef.Raw)
		if len(candidates) == 0 {
			diags = append(diags, diag.Errorf(f.Pos,
				"no provider matches %q", f.ProviderRef.Raw))
			continue
		}
		var matched []*Provider
		for _, p := range candidates {
			if p.Result != nil && types.Identical(p.Result, f.Type) {
				matched = append(matched, p)
			}
		}
		switch len(matched) {
		case 0:
			diags = append(diags, diag.Errorf(f.Pos,
				"provider %q does not produce %s", f.ProviderRef.Raw, TypeString(f.Type)).
				WithHints(FormatCandidates(candidates)...))
		case 1:
			tk := TypeKey(f.Type)
			if existing, dup := out[tk]; dup && existing != matched[0] {
				diags = append(diags, diag.Errorf(f.Pos,
					"conflicting providers selected for %s: %s vs %s (also at %s)",
					TypeString(f.Type),
					ProviderName(existing), ProviderName(matched[0]),
					posByType[tk]))
				continue
			}
			out[tk] = matched[0]
			posByType[tk] = f.Pos
		default:
			diags = append(diags, diag.Errorf(f.Pos,
				"reference %q is ambiguous", f.ProviderRef.Raw).
				WithHints(FormatCandidates(matched)...))
		}
	}
	return out, diags
}
