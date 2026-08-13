package relation

import "go/types"

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
