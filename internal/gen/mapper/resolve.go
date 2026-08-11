package mapper

import (
	"errors"
	"fmt"
	"go/token"
	"go/types"
	"reflect"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// pairSpec is a type pair to generate mappers for, with types fully
// resolved. Src and Dst may be pointers to named structs.
type pairSpec struct {
	Src types.Type
	Dst types.Type
}

// fieldKey identifies a struct field for -exclude matching.
type fieldKey struct {
	PkgPath string
	Type    string
	Field   string
}

// resolveConfig carries everything resolvePlans needs.
type resolveConfig struct {
	Fset      *token.FileSet
	Pairs     []pairSpec
	Conv      converterTable
	Ignores   map[fieldKey]bool
	Direction Direction
}

type resolver struct {
	cfg         resolveConfig
	plans       []*funcPlan
	usedIgnores map[fieldKey]bool
	badTags     map[token.Position]bool
	errs        []error
}

// resolvePlans builds mapping plans for all pairs in the requested
// directions. Errors are collected and reported together.
func resolvePlans(cfg resolveConfig) ([]*funcPlan, error) {
	r := &resolver{
		cfg:         cfg,
		usedIgnores: make(map[fieldKey]bool),
		badTags:     make(map[token.Position]bool),
	}
	r.buildShells()
	for _, p := range r.plans {
		r.resolveFields(p)
	}
	r.finalize()
	if len(r.errs) > 0 {
		return nil, errors.Join(r.errs...)
	}
	return r.plans, nil
}

// buildShells creates empty plans for every pair and direction so nested
// field resolution can reference them before their own fields resolve.
func (r *resolver) buildShells() {
	for _, pair := range r.cfg.Pairs {
		// Validate both sides so one bad pair reports every problem at once.
		srcOK := r.validatePairType(pair.Src)
		dstOK := r.validatePairType(pair.Dst)
		if !srcOK || !dstOK {
			continue
		}
		if r.cfg.Direction != DirectionFrom {
			r.plans = append(r.plans, &funcPlan{name: funcName(pair, false), src: pair.Src, dst: pair.Dst})
		}
		if r.cfg.Direction != DirectionTo {
			r.plans = append(r.plans, &funcPlan{name: funcName(pair, true), src: pair.Dst, dst: pair.Src})
		}
	}
	seen := make(map[string]bool, len(r.plans))
	for _, p := range r.plans {
		if seen[p.name] {
			r.errs = append(r.errs, fmt.Errorf("declared pairs produce duplicate function name %s", p.name))
		}
		seen[p.name] = true
	}
}

func (r *resolver) validatePairType(t types.Type) bool {
	named, st, ok := structNamed(t)
	if !ok {
		r.errs = append(r.errs, fmt.Errorf("type %s is not a struct or pointer to struct", typeLabel(t)))
		return false
	}
	if named.TypeParams().Len() > 0 {
		r.errs = append(r.errs, fmt.Errorf("%s: generic types are not supported", typeLabel(t)))
		return false
	}
	if isOpaque(named, st) {
		r.errs = append(r.errs, fmt.Errorf(
			"%s has no exported fields but has getters: the protobuf opaque API is not supported", typeLabel(t)))
		return false
	}
	return true
}

// structNamed unwraps t to its named struct type, looking through a
// single pointer and any aliases.
func structNamed(t types.Type) (*types.Named, *types.Struct, bool) {
	u := types.Unalias(t)
	if p, ok := u.(*types.Pointer); ok {
		u = types.Unalias(p.Elem())
	}
	named, ok := u.(*types.Named)
	if !ok {
		return nil, nil, false
	}
	st, ok := named.Underlying().(*types.Struct)
	if !ok {
		return nil, nil, false
	}
	return named, st, true
}

func isOpaque(named *types.Named, st *types.Struct) bool {
	for f := range st.Fields() {
		if f.Exported() {
			return false
		}
	}
	for m := range named.Methods() {
		if strings.HasPrefix(m.Name(), "Get") {
			return true
		}
	}
	return false
}

// funcName derives the generated function name from the A-side
// perspective: <AType>To<X> / <AType>From<X>, where X is the B type name,
// or B's package name when the type names collide.
func funcName(pair pairSpec, from bool) string {
	a, _, _ := structNamed(pair.Src)
	b, _, _ := structNamed(pair.Dst)
	x := b.Obj().Name()
	if x == a.Obj().Name() {
		x = titleFirst(b.Obj().Pkg().Name())
	}
	dir := "To"
	if from {
		dir = "From"
	}
	return a.Obj().Name() + dir + x
}

func titleFirst(s string) string {
	r, size := utf8.DecodeRuneInString(s)
	if r == utf8.RuneError {
		return s
	}
	return string(unicode.ToUpper(r)) + s[size:]
}

// srcField is a source struct field visible to matching, with its rename
// tag if any.
type srcField struct {
	v   *types.Var
	tag string
}

func (r *resolver) resolveFields(p *funcPlan) {
	srcNamed, srcStruct, _ := structNamed(p.src)
	dstNamed, dstStruct, _ := structNamed(p.dst)

	srcFields := r.collectSrcFields(srcStruct)
	for i := range dstStruct.NumFields() {
		f := dstStruct.Field(i)
		if !f.Exported() {
			continue
		}
		// -exclude comes first: it is the escape hatch for a destination the
		// author cannot edit, so it has to apply even to a field whose tag this
		// generator would reject, and a map:"-" field still marks its entry used.
		key := fieldKey{PkgPath: dstNamed.Obj().Pkg().Path(), Type: dstNamed.Obj().Name(), Field: f.Name()}
		if r.cfg.Ignores[key] {
			r.usedIgnores[key] = true
			continue
		}
		tag, skip := r.fieldTag(dstStruct, i)
		if skip {
			continue
		}
		chosen, ok := r.matchSrcField(p, srcNamed, f, tag, srcFields)
		if !ok {
			continue
		}
		fp, ok := r.buildFieldPlan(p, f, chosen)
		if !ok {
			continue
		}
		p.fields = append(p.fields, fp)
	}
	r.checkSrcTags(p, srcFields, dstStruct)
}

func (r *resolver) collectSrcFields(st *types.Struct) []srcField {
	var fields []srcField
	for i := range st.NumFields() {
		f := st.Field(i)
		if !f.Exported() {
			continue
		}
		tag, skip := r.fieldTag(st, i)
		if skip {
			continue
		}
		fields = append(fields, srcField{v: f, tag: tag})
	}
	return fields
}

// fieldTag reads and validates the field's map tag. skip reports that the
// field takes no part in mapping (tagged "-" or invalid).
func (r *resolver) fieldTag(st *types.Struct, i int) (string, bool) {
	tag, ok := reflect.StructTag(st.Tag(i)).Lookup("map")
	if !ok {
		return "", false
	}
	if tag == "-" {
		return "", true
	}
	if !token.IsIdentifier(tag) {
		// With both directions generated, a struct is visited once as a source
		// and once as a destination, so report each bad tag only the first time.
		pos := r.pos(st.Field(i))
		if !r.badTags[pos] {
			r.badTags[pos] = true
			r.errs = append(r.errs, fmt.Errorf("%s: invalid map tag %q", pos, tag))
		}
		return "", true
	}
	return tag, false
}

// matchSrcField finds the source field for dst field f.
// Priority: f's own map tag, a source field tagged with f's name, exact
// name match, case-insensitive match, then promoted fields (exact only).
func (r *resolver) matchSrcField(
	p *funcPlan, srcNamed *types.Named, f *types.Var, dstTag string, srcFields []srcField,
) (*types.Var, bool) {
	var chosen *types.Var
	if dstTag != "" {
		for _, sf := range srcFields {
			if sf.v.Name() == dstTag {
				chosen = sf.v
				break
			}
		}
		if chosen == nil {
			r.errs = append(r.errs, fmt.Errorf("%s: map tag %q names a source field that does not exist in %s",
				r.pos(f), dstTag, typeLabel(p.src)))
			return nil, false
		}
	}
	var tagged []*types.Var
	for _, sf := range srcFields {
		if sf.tag == f.Name() {
			tagged = append(tagged, sf.v)
		}
	}
	if len(tagged) > 1 {
		r.errs = append(r.errs, fmt.Errorf("%s: multiple source fields in %s are tagged map:%q",
			r.pos(f), typeLabel(p.src), f.Name()))
		return nil, false
	}
	if len(tagged) == 1 {
		if chosen != nil && chosen != tagged[0] {
			r.errs = append(r.errs, fmt.Errorf(
				"%s: conflicting map tags for %s.%s: the field's tag names %s but source field %s is tagged map:%q",
				r.pos(f), namedLabel(p.dst), f.Name(), dstTag, tagged[0].Name(), f.Name()))
			return nil, false
		}
		chosen = tagged[0]
	}
	if chosen != nil {
		return chosen, true
	}

	var folds []*types.Var
	for _, sf := range srcFields {
		if sf.tag != "" {
			continue
		}
		if sf.v.Name() == f.Name() {
			return sf.v, true
		}
		if strings.EqualFold(sf.v.Name(), f.Name()) {
			folds = append(folds, sf.v)
		}
	}
	if len(folds) == 1 {
		return folds[0], true
	}
	if len(folds) > 1 {
		r.errs = append(r.errs, fmt.Errorf("%s: source field for %s.%s is ambiguous in %s",
			r.pos(f), namedLabel(p.dst), f.Name(), typeLabel(p.src)))
		return nil, false
	}
	if v, ok := r.promotedField(p.src, srcNamed.Obj().Pkg(), f.Name()); ok {
		return v, true
	}
	r.unmappedError(p, f)
	return nil, false
}

// promotedField returns the field named name reached through an embedded
// struct, if it is eligible to be matched by name.
//
// A tag on the promoted field disqualifies it. The tag says the field is not
// simply "name" — map:"-" excludes it, and a rename points it at some other
// destination — and honoring the name anyway would copy a field the author
// asked not to. The caller reports it as unmapped instead, which is at least
// visible.
func (r *resolver) promotedField(src types.Type, pkg *types.Package, name string) (*types.Var, bool) {
	obj, index, _ := types.LookupFieldOrMethod(src, true, pkg, name)
	if obj == nil || len(index) <= 1 {
		return nil, false
	}

	v, ok := obj.(*types.Var)
	if !ok || !v.IsField() || !v.Exported() {
		return nil, false
	}

	st, i, ok := declaringStruct(src, index)
	if !ok {
		return nil, false
	}
	if _, tagged := reflect.StructTag(st.Tag(i)).Lookup("map"); tagged {
		return nil, false
	}

	return v, true
}

// declaringStruct walks an embedding path to the struct that declares the final
// field, and returns it with that field's index.
func declaringStruct(t types.Type, index []int) (*types.Struct, int, bool) {
	_, st, ok := structNamed(t)
	if !ok {
		return nil, 0, false
	}

	for _, i := range index[:len(index)-1] {
		if i >= st.NumFields() {
			return nil, 0, false
		}
		_, next, ok := structNamed(st.Field(i).Type())
		if !ok {
			return nil, 0, false
		}
		st = next
	}

	last := index[len(index)-1]
	if last >= st.NumFields() {
		return nil, 0, false
	}

	return st, last, true
}

func (r *resolver) unmappedError(p *funcPlan, f *types.Var) {
	msg := fmt.Sprintf("%s: no source field in %s for %s.%s", r.pos(f), typeLabel(p.src), namedLabel(p.dst), f.Name())
	if f.Anonymous() {
		msg += "\n\tnote: embedded fields are not flattened on the destination side"
	}
	msg += "\n\tadd a map tag naming the source field, exclude the field with map:\"-\", or pass -exclude"
	r.errs = append(r.errs, errors.New(msg))
}

// buildFieldPlan resolves the conversion for one field.
//
// A read whose type already matches the destination wins, wherever it comes
// from; only then does the getter's result type come before the raw field. The
// order matters for a nil-able field wrapped in a getter that returns a value:
// preferring the getter would turn a nil pointer into a pointer to a zero, which
// is exactly the distinction the pointer was carrying.
func (r *resolver) buildFieldPlan(p *funcPlan, dstField, srcVar *types.Var) (fieldPlan, bool) {
	cands := readCandidates(p.src, srcVar)
	dst := types.Unalias(dstField.Type())

	for _, read := range cands {
		if types.Identical(types.Unalias(read.typ), dst) {
			return fieldPlan{dstName: dstField.Name(), dstType: dstField.Type(), read: read, conv: opDirect{}}, true
		}
	}

	for _, read := range cands {
		if conv, err := r.resolveOp(read.typ, dstField.Type()); err == nil {
			return fieldPlan{dstName: dstField.Name(), dstType: dstField.Type(), read: read, conv: conv}, true
		}
	}

	r.conversionError(p, dstField, srcVar)
	return fieldPlan{}, false
}

func readCandidates(src types.Type, field *types.Var) []readAccess {
	var cands []readAccess
	obj, _, _ := types.LookupFieldOrMethod(src, true, field.Pkg(), "Get"+field.Name())
	if m, ok := obj.(*types.Func); ok {
		sig := m.Signature()
		if sig.Params().Len() == 0 && sig.Results().Len() == 1 {
			cands = append(cands, readAccess{name: m.Name(), getter: true, typ: sig.Results().At(0).Type()})
		}
	}
	return append(cands, readAccess{name: field.Name(), typ: field.Type()})
}

func (r *resolver) conversionError(p *funcPlan, dstField, srcVar *types.Var) {
	srcT, dstT := srcVar.Type(), dstField.Type()
	var b strings.Builder
	fmt.Fprintf(&b, "%s: cannot map %s.%s (%s) to %s.%s (%s)",
		r.pos(dstField), namedLabel(p.src), srcVar.Name(), typeLabel(srcT),
		namedLabel(p.dst), dstField.Name(), typeLabel(dstT))
	fmt.Fprintf(&b, "\n\tregister a converter: mapper.Register(func(%s) %s { ... })", typeLabel(srcT), typeLabel(dstT))
	b.WriteString("\n\tor declare the pair in -types, or exclude the field with map:\"-\" or -ignore")
	if isInterface(srcT) || isInterface(dstT) {
		b.WriteString("\n\tnote: interface-typed fields (protobuf oneof) are not supported")
	}
	r.errs = append(r.errs, errors.New(b.String()))
}

func isInterface(t types.Type) bool {
	_, ok := types.Unalias(t).Underlying().(*types.Interface)
	return ok
}

// checkSrcTags reports source rename tags that name no mappable
// destination field. Unexported destination fields do not count: a tag
// pointing at one would silently do nothing.
func (r *resolver) checkSrcTags(p *funcPlan, srcFields []srcField, dstStruct *types.Struct) {
	names := make(map[string]bool, dstStruct.NumFields())
	for f := range dstStruct.Fields() {
		if f.Exported() {
			names[f.Name()] = true
		}
	}
	for _, sf := range srcFields {
		if sf.tag != "" && !names[sf.tag] {
			r.errs = append(r.errs, fmt.Errorf("%s: map tag %q names a field that does not exist in %s",
				r.pos(sf.v), sf.tag, namedLabel(p.dst)))
		}
	}
}

// resolveOp finds a conversion from src to dst, in priority order:
// identity, registered converter, declared pair, source deref,
// destination address-of, element-wise slice, restricted type conversion.
// Source deref is tried before destination address-of so that a nil
// pointer maps to a nil pointer, not a pointer to a zero value.
func (r *resolver) resolveOp(src, dst types.Type) (op, error) {
	src, dst = types.Unalias(src), types.Unalias(dst)
	if types.Identical(src, dst) {
		return opDirect{}, nil
	}
	if c, ok := r.cfg.Conv.lookup(src, dst); ok {
		return opConvert{conv: c}, nil
	}
	if p := r.planFor(src, dst); p != nil {
		return opMapper{plan: p}, nil
	}
	if sp, ok := src.(*types.Pointer); ok {
		if elem, err := r.resolveOp(sp.Elem(), dst); err == nil {
			return opDeref{elem: elem}, nil
		}
	}
	if dp, ok := dst.(*types.Pointer); ok {
		if elem, err := r.resolveOp(src, dp.Elem()); err == nil {
			return opAddr{elem: elem}, nil
		}
	}
	if ss, ok := src.Underlying().(*types.Slice); ok {
		if ds, ok := dst.Underlying().(*types.Slice); ok {
			if elem, err := r.resolveOp(ss.Elem(), ds.Elem()); err == nil {
				return opSlice{dst: dst, elem: elem}, nil
			}
		}
	}
	if typeConvertible(src, dst) {
		return opTypeConv{dst: dst}, nil
	}
	return nil, fmt.Errorf("no conversion from %s to %s", typeLabel(src), typeLabel(dst))
}

func (r *resolver) planFor(src, dst types.Type) *funcPlan {
	for _, p := range r.plans {
		if types.Identical(p.src, src) && types.Identical(p.dst, dst) {
			return p
		}
	}
	return nil
}

// typeConvertible reports whether a plain Go conversion dst(v) is both
// legal and loss-free on every platform. Narrowing, sign-changing,
// precision-losing, and surprising conversions (numeric to string, float
// to integer) are excluded; they require a converter.
func typeConvertible(src, dst types.Type) bool {
	su, du := src.Underlying(), dst.Underlying()
	if types.Identical(su, du) && types.ConvertibleTo(src, dst) {
		return true
	}
	if sb, ok := su.(*types.Basic); ok {
		db, ok := du.(*types.Basic)
		if !ok {
			return isString(su) && isByteSlice(du)
		}
		return numericWidening(sb, db)
	}
	return isByteSlice(su) && isString(du)
}

// numericWidening reports whether every value of sb is exactly
// representable in db on all platforms.
func numericWidening(sb, db *types.Basic) bool {
	if db.Info()&types.IsFloat != 0 {
		if sb.Info()&types.IsFloat != 0 {
			return sb.Kind() == types.Float32 && db.Kind() == types.Float64
		}
		_, sMax, sSigned, ok := intRange(sb.Kind())
		if !ok {
			return false
		}
		mantissa := 24
		if db.Kind() == types.Float64 {
			mantissa = 53
		}
		magnitude := sMax
		if sSigned {
			magnitude--
		}
		return magnitude <= mantissa
	}
	_, sMax, sSigned, sOK := intRange(sb.Kind())
	dMin, _, dSigned, dOK := intRange(db.Kind())
	if !sOK || !dOK {
		return false
	}
	switch {
	case sSigned && !dSigned:
		return false
	case sSigned == dSigned:
		return sMax <= dMin
	default: // unsigned source into signed destination needs one extra bit
		return sMax < dMin
	}
}

// intRange returns the guaranteed minimum and possible maximum bit
// widths of an integer kind across platforms. int and uint are 32 bits
// on some platforms and 64 on others; uintptr always needs a converter.
func intRange(k types.BasicKind) (minBits, maxBits int, signed, ok bool) {
	//nolint:exhaustive // Every other kind deliberately reports !ok: it requires a converter.
	switch k {
	case types.Int8:
		return 8, 8, true, true
	case types.Int16:
		return 16, 16, true, true
	case types.Int32:
		return 32, 32, true, true
	case types.Int64:
		return 64, 64, true, true
	case types.Int:
		return 32, 64, true, true
	case types.Uint8:
		return 8, 8, false, true
	case types.Uint16:
		return 16, 16, false, true
	case types.Uint32:
		return 32, 32, false, true
	case types.Uint64:
		return 64, 64, false, true
	case types.Uint:
		return 32, 64, false, true
	default:
		return 0, 0, false, false
	}
}

func isString(t types.Type) bool {
	b, ok := t.(*types.Basic)
	return ok && b.Info()&types.IsString != 0
}

func isByteSlice(t types.Type) bool {
	s, ok := t.(*types.Slice)
	if !ok {
		return false
	}
	b, ok := types.Unalias(s.Elem()).Underlying().(*types.Basic)
	return ok && b.Kind() == types.Uint8
}

// finalize propagates error-returning through nested mapper calls until
// stable and reports -ignore entries that matched nothing.
func (r *resolver) finalize() {
	for changed := true; changed; {
		changed = false
		for _, p := range r.plans {
			if p.returnsError {
				continue
			}
			for _, f := range p.fields {
				if f.conv.mayFail() {
					p.returnsError = true
					changed = true
					break
				}
			}
		}
	}

	var unused []fieldKey
	for key := range r.cfg.Ignores {
		if !r.usedIgnores[key] {
			unused = append(unused, key)
		}
	}
	slices.SortFunc(unused, func(a, b fieldKey) int {
		return strings.Compare(a.PkgPath+a.Type+a.Field, b.PkgPath+b.Type+b.Field)
	})
	for _, key := range unused {
		r.errs = append(r.errs, fmt.Errorf("-ignore entry %s.%s.%s matched nothing", key.PkgPath, key.Type, key.Field))
	}
}

func (r *resolver) pos(obj types.Object) token.Position {
	return r.cfg.Fset.Position(obj.Pos())
}

// namedLabel renders the named struct type behind t (unwrapping a
// pointer) for error messages.
func namedLabel(t types.Type) string {
	named, _, ok := structNamed(t)
	if !ok {
		return typeLabel(t)
	}
	return named.Obj().Pkg().Name() + "." + named.Obj().Name()
}
