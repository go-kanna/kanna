// Package converters registers the type converters kanna-mapper wires into the
// generated code, and exposes them for manual use too.
package converters

import (
	"time"

	"github.com/google/uuid"

	"github.com/go-kanna/kanna/examples/mapper/gen/employeev1"
	"github.com/go-kanna/kanna/mapper"
)

func init() {
	mapper.Register(ToDate)
	mapper.Register(UUIDToString)
	mapper.Register(ToTime)
	mapper.RegisterE(uuid.Parse)
}

// UUIDToString renders a UUID in its canonical string form.
func UUIDToString(id uuid.UUID) string {
	return id.String()
}

// ToTime converts a calendar date into a UTC midnight time.
func ToTime(d *employeev1.Date) time.Time {
	return time.Date(int(d.GetYear()), time.Month(d.GetMonth()), int(d.GetDay()), 0, 0, 0, 0, time.UTC)
}

// ToDate converts a time into its calendar date.
func ToDate(t time.Time) *employeev1.Date {
	year, month, day := t.Date()
	//nolint:gosec // Calendar year, month, and day always fit in int32.
	return &employeev1.Date{Year: int32(year), Month: int32(month), Day: int32(day)}
}
