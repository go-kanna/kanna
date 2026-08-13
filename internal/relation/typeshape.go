package relation

import "go/types"

// RelationShape reports whether an untagged field's type looks like a
// relation rather than a column: a slice of structs or a pointer to one,
// unless the struct itself reads as a column.
func RelationShape(t types.Type) bool {
	core := t
	if elem, isSlice := SliceElem(core); isSlice {
		core = elem
	} else if elem, isPtr := PointerElem(core); isPtr {
		core = elem
	} else {
		return false
	}
	if elem, isPtr := PointerElem(core); isPtr {
		core = elem
	}
	named, ok := StructNamed(core)
	return ok && !columnLike(named)
}

// columnLike reports whether a struct-kind named type still reads as a
// column: time.Time, or anything carrying a Scan method for the driver to
// hand a value through.
func columnLike(named *types.Named) bool {
	if obj := named.Obj(); obj.Name() == "Time" && obj.Pkg() != nil && obj.Pkg().Path() == "time" {
		return true
	}
	for m := range types.NewMethodSet(types.NewPointer(named)).Methods() {
		sig, ok := m.Obj().Type().(*types.Signature)
		if !ok || m.Obj().Name() != "Scan" || sig.Params().Len() != 1 || sig.Results().Len() != 1 {
			continue
		}
		// The sql.Scanner contract exactly: Scan(any) error. Anything else
		// named Scan is not something database/sql can hand a value to.
		param, isInterface := sig.Params().At(0).Type().Underlying().(*types.Interface)
		if !isInterface || !param.Empty() {
			continue
		}
		if types.Identical(sig.Results().At(0).Type(), types.Universe.Lookup("error").Type()) {
			return true
		}
	}
	return false
}

// SliceElem returns the element type when t's underlying type is a slice.
func SliceElem(t types.Type) (types.Type, bool) {
	s, ok := t.Underlying().(*types.Slice)
	if !ok {
		return nil, false
	}
	return s.Elem(), true
}

// PointerElem returns the element type when t's underlying type is a pointer.
func PointerElem(t types.Type) (types.Type, bool) {
	p, ok := t.Underlying().(*types.Pointer)
	if !ok {
		return nil, false
	}
	return p.Elem(), true
}

// StructNamed returns the named type behind t, unaliased, when its underlying
// type is a struct.
func StructNamed(t types.Type) (*types.Named, bool) {
	named, ok := types.Unalias(t).(*types.Named)
	if !ok {
		return nil, false
	}
	_, ok = named.Underlying().(*types.Struct)
	return named, ok
}
