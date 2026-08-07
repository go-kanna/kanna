package di

import (
	"fmt"
	"go/types"
	"strings"
	"unicode"
)

func deriveInputName(t types.Type) string {
	if t == nil {
		return "arg"
	}
	// Resolve type aliases (`type Client = valkey.Client`) so the alias's own
	// name flows through instead of falling out to the "arg" sentinel.
	t = types.Unalias(t)
	if ptr, ok := t.(*types.Pointer); ok {
		return deriveInputName(ptr.Elem())
	}
	if named, ok := t.(*types.Named); ok {
		if obj := named.Obj(); obj != nil && obj.Name() != "" {
			return lowerFirst(obj.Name())
		}
	}
	return "arg"
}

// renameOutputSteps renames non-input steps that produce a container output to
// lowerFirst(field name). This puts the field's own identifier at the call site
// (`tx := tx.New(...)` for a `Tx` field, suffixed if it would shadow an existing
// step name). Steps that are not bound to any output keep the type-derived name
// picked at resolution time.
//
// Renaming runs in two passes so that an output whose desired base name is
// currently held by another step that is also about to be renamed can claim the
// now-vacated name without unnecessarily suffixing. Without this, a hypothetical
// swap (step A holds "foo" and wants "db", step B holds "db" and wants "foo")
// would land on `foo2`/`db` instead of the clean `foo`/`db`.
func renameOutputSteps(steps []Step, outputs []Output) {
	used := make(map[string]bool, len(steps))
	for _, st := range steps {
		used[st.VarName] = true
	}

	type pendingRename struct {
		stepIdx int
		base    string
	}
	var renames []pendingRename
	// A shared step (bound to more than one container field, e.g. when two
	// fields request the same dependency) appears multiple times in outputs.
	// Decide the rename for each step at the first valid output and skip any
	// later occurrences so we don't queue the same step twice, which would leak
	// the dropped candidate name into `used` and force unrelated steps onto a
	// suffix.
	decided := make(map[int]bool, len(outputs))
	for _, o := range outputs {
		if o.StepIndex < 0 || o.StepIndex >= len(steps) {
			continue
		}
		if decided[o.StepIndex] {
			continue
		}
		s := &steps[o.StepIndex]
		if s.Kind == StepKindInput {
			decided[o.StepIndex] = true
			continue
		}
		base := lowerFirst(o.FieldName)
		if base == "" {
			continue
		}
		if base == s.VarName {
			// Existing name already lines up with this field; no rename needed
			// even if later outputs would have picked a different base.
			decided[o.StepIndex] = true
			continue
		}
		delete(used, s.VarName)
		renames = append(renames, pendingRename{stepIdx: o.StepIndex, base: base})
		decided[o.StepIndex] = true
	}

	for _, r := range renames {
		steps[r.stepIdx].VarName = uniqueName(r.base, used)
		used[steps[r.stepIdx].VarName] = true
	}
}

func varNameForEmbed(es embedSource, existing []Step) string {
	// FieldName may be a dotted selector (e.g. "CommonInfra.DB") when the source
	// comes from a promoted field; only the leaf segment is a valid identifier
	// base.
	leaf := es.FieldName
	if i := strings.LastIndex(leaf, "."); i >= 0 {
		leaf = leaf[i+1:]
	}
	base := lowerFirst(leaf)
	if base == "" {
		base = "v"
	}
	return uniqueName(base, usedVarNames(existing))
}

func varNameForProvider(p *Provider, existing []Step) string {
	// Name the variable after what the call produces, not after the constructor
	// function. `db.Open(...) *sql.DB` reads more naturally as
	// `db := db.Open(...)` than `open := db.Open(...)`, and
	// container-field-bound steps later get renamed once more to the destination
	// field name.
	base := deriveInputName(p.Result)
	if base == "arg" {
		// Anonymous or unnamed result type — fall back to the function name for
		// a less generic label than "arg".
		base = p.FuncName
		if strings.HasPrefix(base, "New") && len(base) > 3 {
			base = base[3:]
		}
		base = lowerFirst(base)
	}
	if base == "" {
		base = "v"
	}

	return uniqueName(base, usedVarNames(existing))
}

func usedVarNames(steps []Step) map[string]bool {
	used := make(map[string]bool, len(steps))
	for _, s := range steps {
		used[s.VarName] = true
	}
	return used
}

// uniqueName returns base, or base with the lowest numeric suffix from 2 that is
// not already taken.
func uniqueName(base string, used map[string]bool) string {
	if !used[base] {
		return base
	}
	for i := 2; ; i++ {
		try := fmt.Sprintf("%s%d", base, i)
		if !used[try] {
			return try
		}
	}
}

func upperFirst(s string) string {
	if s == "" {
		return ""
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

// lowerFirst lowercases the leading run of uppercase letters of s, following the
// Go convention that "URL" becomes "url" and "URLPath" becomes "urlPath" (the
// last cap of a leading run is preserved when followed by a lowercase letter).
func lowerFirst(s string) string {
	if s == "" {
		return ""
	}
	runes := []rune(s)
	n := len(runes)
	i := 0
	for i < n && unicode.IsUpper(runes[i]) {
		i++
	}
	if i == 0 {
		return s
	}
	if i < n && i > 1 {
		i--
	}
	for j := range i {
		runes[j] = unicode.ToLower(runes[j])
	}
	return string(runes)
}
