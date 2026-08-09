// Package errcases holds type pairs that must fail plan resolution, one
// pair per rule.
package errcases

import "time"

// UnmappedSrc lacks a counterpart for UnmappedDst.Bar.
type UnmappedSrc struct{ Foo string }

// UnmappedDst has a field with no source.
type UnmappedDst struct{ Foo, Bar string }

// TagMissingSrc has no field named Nope.
type TagMissingSrc struct{ X string }

// TagMissingDst tags its field to a nonexistent counterpart.
type TagMissingDst struct {
	X string `map:"Nope"`
}

// ConflictSrc tags A to Out while ConflictDst.Out names B.
type ConflictSrc struct {
	A string `map:"Out"`
	B string
}

// ConflictDst declares the other half of the tag conflict.
type ConflictDst struct {
	Out string `map:"B"`
}

// AmbiguousSrc fold-matches AmbiguousDst.Ident twice.
type AmbiguousSrc struct{ IDent, IdEnt string }

// AmbiguousDst is the ambiguous match target.
type AmbiguousDst struct{ Ident string }

// OneofSrc pairs with OneofDst.
type OneofSrc struct{ Payload string }

// OneofDst has an interface field like a protobuf oneof.
type OneofDst struct{ Payload isPayload }

type isPayload interface{ isPayload() }

// OpaqueSrc pairs with OpaqueDst.
type OpaqueSrc struct{ Id string }

// OpaqueDst mimics the protobuf opaque API: no exported fields, getters
// only.
type OpaqueDst struct{ id string }

// GetId returns the hidden field.
func (o *OpaqueDst) GetId() string { return o.id }

// BadTagSrc pairs with BadTagDst.
type BadTagSrc struct{ X string }

// BadTagDst carries an invalid map tag value.
type BadTagDst struct {
	X string `map:"9x"`
}

// LossySrc pairs with LossyDst; float64 to int must not auto-convert.
type LossySrc struct{ V float64 }

// LossyDst is the lossy conversion target.
type LossyDst struct{ V int }

// TagUnexportedSrc tags its field to an unexported counterpart, which
// can never be mapped.
type TagUnexportedSrc struct {
	A string `map:"hidden"`
}

// TagUnexportedDst has only the unexported field the tag points at.
type TagUnexportedDst struct{ hidden string }

// NarrowSrc pairs with NarrowDst; int64 to int32 narrows and must not
// auto-convert.
type NarrowSrc struct{ V int64 }

// NarrowDst is the narrowing target.
type NarrowDst struct{ V int32 }

// SignSrc pairs with SignDst; int to uint changes sign and must not
// auto-convert.
type SignSrc struct{ V int }

// SignDst is the sign-change target.
type SignDst struct{ V uint }

// NeedConvSrc pairs with NeedConvDst; time.Time to string requires a
// converter.
type NeedConvSrc struct{ When time.Time }

// NeedConvDst is the suggestion-message target.
type NeedConvDst struct{ When string }
