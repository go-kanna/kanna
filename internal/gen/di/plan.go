package di

import (
	"fmt"
	"go/token"
	"go/types"

	"github.com/go-kanna/kanna/internal/diag"
)

// Options carries CLI-level defaults that a //kanna:container directive may
// override per container.
type Options struct {
	// Must is the default applied to containers whose directive does not
	// explicitly specify must.
	Must bool
}

// Plan is the resolved sequence of operations needed to construct a single
// container, plus metadata used by the emit layer.
//
// ReturnType is the *declared* return type of the constructor. The emitter
// always produces &<StructName>{...} as the return expression and relies on Go's
// assignability rules to fit ReturnType — so when ReturnType differs from
// *<StructName> (e.g. via di:"returns" or //kanna:container returns=...),
// *<StructName> must implement (or be identical to) ReturnType. This is also why
// RoleReturnsOnly fields contribute no Step: their type is recorded for the
// signature but the value is supplied by the container struct literal itself.
type Plan struct {
	Container       Container
	ConstructorName string
	ReturnType      types.Type
	EmitMust        bool
	ReturnsError    bool

	// Inputs are constructor parameters in container-field order.
	Inputs []Input
	// Steps are resolution operations in execution order (deps first).
	Steps []Step
	// Outputs map RoleOut field names to the step that produces their value.
	Outputs []Output
}

// StepKind classifies a step in a Plan.
type StepKind int

const (
	// StepKindProvider invokes a provider function with the given args.
	StepKindProvider StepKind = iota
	// StepKindInput refers to a constructor input parameter.
	StepKindInput
	// StepKindEmbedField refers to an exported field of a di:"embed" input,
	// accessed as <input>.<FieldName>.
	StepKindEmbedField
)

// Step is a single resolution operation.
type Step struct {
	Kind    StepKind
	VarName string
	OutType types.Type

	// For StepKindProvider:
	Provider *Provider
	ArgSteps []int

	// For StepKindInput and StepKindEmbedField:
	InputIndex int

	// For StepKindEmbedField:
	EmbedFieldName string
}

// Input is a constructor parameter (declared via di:"arg" or di:"embed").
type Input struct {
	Name string
	Type types.Type
}

// Output is a RoleOut field assignment: which step's value goes into which
// container field.
type Output struct {
	FieldName string
	StepIndex int
}

// Build resolves dependencies for one container against the given provider index
// and CLI defaults, returning a Plan that the emit layer can consume.
func Build(c Container, idx *Index, opts Options) (Plan, []diag.Diag) {
	var diags []diag.Diag

	constructorName := constructorNameFor(c)
	returnType, retDiag := resolveReturnType(c)
	if retDiag != nil {
		diags = append(diags, *retDiag)
	}
	emitMust := mergeMust(c.Directive.Must, opts.Must)

	inputs, inDiags := buildInputs(c)
	diags = append(diags, inDiags...)

	overrides, ovDiags := buildOverrides(c, idx)
	diags = append(diags, ovDiags...)

	embeds, emDiags := buildEmbeds(c, inputs)
	diags = append(diags, emDiags...)

	r := &resolver{
		idx:          idx,
		inputs:       inputs,
		overrides:    overrides,
		embeds:       embeds,
		stepByKey:    map[string]int{},
		active:       map[string]bool{},
		selfPkgPath:  c.PkgPath,
		selfFuncName: constructorName,
	}

	var outputs []Output
	for _, f := range c.Fields {
		switch f.Role {
		case RoleOut:
			stepIdx, ds := r.resolveField(f)
			diags = append(diags, ds...)
			if stepIdx < 0 {
				continue
			}
			outputs = append(outputs, Output{FieldName: f.Name, StepIndex: stepIdx})
		case RoleArg:
			if f.Name == "_" {
				continue
			}
			stepIdx, ds := r.resolveByType(f.Type, f.Pos, "field "+f.Name)
			diags = append(diags, ds...)
			if stepIdx < 0 {
				continue
			}
			outputs = append(outputs, Output{FieldName: f.Name, StepIndex: stepIdx})
		case RoleOverride, RoleReturnsOnly, RoleEmbed:
			// Handled in buildOverrides / resolveReturnType / buildEmbeds.
		}
	}

	renameOutputSteps(r.steps, outputs)

	returnsErr := false
	for _, s := range r.steps {
		if s.Kind == StepKindProvider && s.Provider != nil && s.Provider.ReturnsError {
			returnsErr = true
			break
		}
	}

	return Plan{
		Container:       c,
		ConstructorName: constructorName,
		ReturnType:      returnType,
		EmitMust:        emitMust,
		ReturnsError:    returnsErr,
		Inputs:          inputs,
		Steps:           r.steps,
		Outputs:         outputs,
	}, diags
}

// resolver holds mutable state during resolution.
type resolver struct {
	idx       *Index
	inputs    []Input
	overrides map[string]*Provider   // typeKey → provider
	embeds    map[string]embedSource // typeKey → embed source
	steps     []Step
	stepByKey map[string]int
	active    map[string]bool

	// selfPkgPath and selfFuncName identify this container's own generated
	// constructor. They are used to filter the by-type lookup so a container
	// whose di:"returns" declares an interface that matches an unrelated
	// provider does not see its own previously emitted constructor as a
	// candidate (a self-loop that would also produce a spurious "multiple
	// providers" error).
	selfPkgPath  string
	selfFuncName string
}

// embedSource describes one exported field of a di:"embed" input that is exposed
// as a resolution source.
type embedSource struct {
	InputIndex int
	FieldName  string
	FieldType  types.Type
}

func (r *resolver) resolveField(f Field) (int, []diag.Diag) {
	if f.ProviderRef.HasRef() {
		return r.resolveByRef(f.Type, f.ProviderRef.Raw, f.Pos)
	}
	return r.resolveByType(f.Type, f.Pos, "field "+f.Name)
}

// excludeSelfProvider drops this container's own constructors from the candidate
// list, so a di:"returns" field does not resolve to the very thing being
// generated. Callers that name a provider explicitly via di:"with=..." are not
// affected.
//
// Both New* and its Must* variant are emitted for the same container and return
// the same type, so both have to go: leaving MustNew* in makes the second run
// over an unchanged tree fail with a spurious ambiguity.
func (r *resolver) excludeSelfProvider(candidates []*Provider) []*Provider {
	if r.selfFuncName == "" {
		return candidates
	}

	kept := make([]*Provider, 0, len(candidates))
	for _, c := range candidates {
		if c.PkgPath == r.selfPkgPath && r.isOwnConstructor(c.FuncName) {
			continue
		}
		kept = append(kept, c)
	}
	return kept
}

// isOwnConstructor reports whether name is one of the constructors this
// container generates.
func (r *resolver) isOwnConstructor(name string) bool {
	return name == r.selfFuncName || name == "Must"+r.selfFuncName
}

func (r *resolver) resolveByType(want types.Type, pos token.Position, parent string) (int, []diag.Diag) {
	tk := TypeKey(want)

	for i, in := range r.inputs {
		if TypeKey(in.Type) == tk {
			return r.useInput(i), nil
		}
	}

	if p, ok := r.overrides[tk]; ok {
		return r.resolveProvider(p, pos)
	}

	if es, ok := r.embeds[tk]; ok {
		return r.useEmbed(es), nil
	}

	candidates := r.excludeSelfProvider(r.idx.LookupByType(want))
	if len(candidates) == 0 {
		return -1, []diag.Diag{
			diag.Errorf(pos, "no provider for %s (required by %s)", TypeString(want), parent),
		}
	}
	if len(candidates) > 1 {
		return -1, []diag.Diag{
			diag.Errorf(pos, "multiple providers for %s (required by %s)", TypeString(want), parent).
				WithHints(FormatCandidates(candidates)...),
		}
	}
	return r.resolveProvider(candidates[0], pos)
}

func (r *resolver) resolveByRef(want types.Type, ref string, pos token.Position) (int, []diag.Diag) {
	candidates := r.idx.LookupByRef(ref)
	if len(candidates) == 0 {
		return -1, []diag.Diag{
			diag.Errorf(pos, "no provider matches %q", ref),
		}
	}

	var matched []*Provider
	for _, p := range candidates {
		if p.Result != nil && types.Identical(p.Result, want) {
			matched = append(matched, p)
		}
	}
	if len(matched) == 0 {
		return -1, []diag.Diag{
			diag.Errorf(pos, "provider %q does not produce %s", ref, TypeString(want)).
				WithHints(FormatCandidates(candidates)...),
		}
	}
	if len(matched) > 1 {
		return -1, []diag.Diag{
			diag.Errorf(pos, "reference %q is ambiguous", ref).
				WithHints(FormatCandidates(matched)...),
		}
	}
	return r.resolveProvider(matched[0], pos)
}

func (r *resolver) useInput(idx int) int {
	key := fmt.Sprintf("input:%d", idx)
	if id, ok := r.stepByKey[key]; ok {
		return id
	}
	in := r.inputs[idx]
	r.steps = append(r.steps, Step{
		Kind:       StepKindInput,
		VarName:    in.Name,
		OutType:    in.Type,
		InputIndex: idx,
	})
	id := len(r.steps) - 1
	r.stepByKey[key] = id
	return id
}

func (r *resolver) useEmbed(es embedSource) int {
	key := fmt.Sprintf("embed:%d:%s", es.InputIndex, es.FieldName)
	if id, ok := r.stepByKey[key]; ok {
		return id
	}
	r.steps = append(r.steps, Step{
		Kind:           StepKindEmbedField,
		VarName:        varNameForEmbed(es, r.steps),
		OutType:        es.FieldType,
		InputIndex:     es.InputIndex,
		EmbedFieldName: es.FieldName,
	})
	id := len(r.steps) - 1
	r.stepByKey[key] = id
	return id
}

func (r *resolver) resolveProvider(p *Provider, pos token.Position) (int, []diag.Diag) {
	key := "provider:" + ProviderName(p)
	if id, ok := r.stepByKey[key]; ok {
		return id, nil
	}
	if r.active[key] {
		return -1, []diag.Diag{
			diag.Errorf(pos, "circular dependency at %s", ProviderName(p)),
		}
	}
	r.active[key] = true
	defer delete(r.active, key)

	var (
		argIDs []int
		diags  []diag.Diag
	)
	for _, pt := range p.Params {
		argID, ds := r.resolveByType(pt, pos, ProviderName(p))
		diags = append(diags, ds...)
		if argID < 0 {
			return -1, diags
		}
		argIDs = append(argIDs, argID)
	}

	r.steps = append(r.steps, Step{
		Kind:     StepKindProvider,
		VarName:  varNameForProvider(p, r.steps),
		OutType:  p.Result,
		Provider: p,
		ArgSteps: argIDs,
	})
	id := len(r.steps) - 1
	r.stepByKey[key] = id
	return id, diags
}
