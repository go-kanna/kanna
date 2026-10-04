// Package optional mimics the proto3-optional shape: a pointer field whose
// nil-ness is meaningful, wrapped in a getter that returns a value.
package optional

import (
	"strconv"

	"github.com/go-kanna/kanna/mapper"
)

func init() {
	mapper.RegisterE(strconv.Atoi)
	mapper.Register(StampSeconds)
	mapper.Register(StampDays)
}

// Stamp stands in for a message type such as timestamppb.Timestamp.
type Stamp struct {
	Seconds int64
}

// StampSeconds takes the pointer the wire side holds, like a converter
// registered for *timestamppb.Timestamp.
func StampSeconds(s *Stamp) int64 {
	return s.Seconds
}

// StampDays converts between the pointers themselves, so it decides what nil
// becomes.
func StampDays(s *Stamp) *int32 {
	if s == nil {
		return nil
	}
	days := int32(s.Seconds / 86400)
	return &days
}

// Wire is the generated-looking side.
type Wire struct {
	Note  *string
	Count *string
	Seen  *Stamp
	Days  *Stamp
}

// GetNote is nil-safe and therefore loses the distinction the pointer carries.
func (x *Wire) GetNote() string {
	if x != nil && x.Note != nil {
		return *x.Note
	}
	return ""
}

// GetCount is nil-safe like GetNote.
func (x *Wire) GetCount() string {
	if x != nil && x.Count != nil {
		return *x.Count
	}
	return ""
}

// Domain keeps the pointers.
type Domain struct {
	Note  *string
	Count *int
	Seen  *int64
	Days  *int32
}
