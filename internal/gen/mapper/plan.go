package mapper

import (
	"fmt"
	"go/types"
	"strings"
)

// funcPlan describes one generated mapping function.
type funcPlan struct {
	name         string
	src          types.Type
	dst          types.Type
	returnsError bool
	fields       []fieldPlan
}

// fieldPlan describes how one destination field is populated.
type fieldPlan struct {
	dstName string
	dstType types.Type
	read    readAccess
	conv    op
}

// readAccess describes how the source value is read: a plain field access
// or a nil-safe getter call.
type readAccess struct {
	name   string
	getter bool
	typ    types.Type // type the read yields
}

// op describes how a source value becomes a destination value. Ops nest
// for composite conversions (pointers, slices).
type op interface {
	mayFail() bool
}

// opDirect assigns the value as-is.
type opDirect struct{}

// opConvert calls a registered converter.
type opConvert struct{ conv converter }

// opMapper calls another generated mapping function.
type opMapper struct{ plan *funcPlan }

// opTypeConv applies a Go type conversion.
type opTypeConv struct{ dst types.Type }

// opDeref dereferences a source pointer, leaving the destination zero
// when the source is nil.
type opDeref struct{ elem op }

// opAddr stores the converted value in a temporary and takes its address.
type opAddr struct{ elem op }

// opSlice converts a slice element-wise; nil maps to nil.
type opSlice struct {
	dst  types.Type // destination slice type
	elem op
}

// Asserted on values, not pointers: every op is constructed as a value, and a
// pointer assertion would keep passing if a method moved to a pointer receiver
// while the construction sites stopped satisfying the interface.
var (
	_ op = opDirect{}
	_ op = opConvert{}
	_ op = opMapper{}
	_ op = opTypeConv{}
	_ op = opDeref{}
	_ op = opAddr{}
	_ op = opSlice{}
)

func (opDirect) mayFail() bool    { return false }
func (o opConvert) mayFail() bool { return o.conv.hasErr }
func (o opMapper) mayFail() bool  { return o.plan.returnsError }
func (opTypeConv) mayFail() bool  { return false }
func (o opDeref) mayFail() bool   { return o.elem.mayFail() }
func (o opAddr) mayFail() bool    { return o.elem.mayFail() }
func (o opSlice) mayFail() bool   { return o.elem.mayFail() }

// describe renders a compact, human-readable form of the plan for tests
// and debug output.
func (p *funcPlan) describe() string {
	var b strings.Builder
	ret := typeLabel(p.dst)
	if p.returnsError {
		ret = "(" + ret + ", error)"
	}
	fmt.Fprintf(&b, "%s(%s) %s", p.name, typeLabel(p.src), ret)
	for _, f := range p.fields {
		read := "." + f.read.name
		if f.read.getter {
			read += "()"
		}
		fmt.Fprintf(&b, "\n  %s = %s %s", f.dstName, read, describeOp(f.conv))
	}
	return b.String()
}

func describeOp(o op) string {
	switch v := o.(type) {
	case opDirect:
		return "direct"
	case opConvert:
		kind := "conv"
		if v.conv.hasErr {
			kind = "convE"
		}
		return kind + ":" + v.conv.fn.Name()
	case opMapper:
		return "map:" + v.plan.name
	case opTypeConv:
		return "cast:" + typeLabel(v.dst)
	case opDeref:
		return "deref(" + describeOp(v.elem) + ")"
	case opAddr:
		return "addr(" + describeOp(v.elem) + ")"
	case opSlice:
		return "slice(" + describeOp(v.elem) + ")"
	default:
		return fmt.Sprintf("unknown(%T)", o)
	}
}
