// Package conv registers converters between model and protolike types
// for resolver tests.
package conv

import (
	"strconv"
	"time"

	"github.com/go-kanna/kanna/internal/gen/mapper/testdata/model"
	"github.com/go-kanna/kanna/internal/gen/mapper/testdata/protolike"
	"github.com/go-kanna/kanna/mapper"
)

func init() {
	mapper.Register(FormatUserID)
	mapper.RegisterE(ParseUserID)
	mapper.Register(ToDate)
	mapper.Register(ToTime)
}

// FormatUserID renders a UserID in decimal form.
func FormatUserID(id model.UserID) string {
	return strconv.FormatInt(int64(id), 10)
}

// ParseUserID parses a decimal UserID.
func ParseUserID(s string) (model.UserID, error) {
	n, err := strconv.ParseInt(s, 10, 64)
	return model.UserID(n), err
}

// ToDate converts a time into its calendar date.
func ToDate(t time.Time) *protolike.Date {
	year, month, day := t.Date()
	return &protolike.Date{Year: int32(year), Month: int32(month), Day: int32(day)}
}

// ToTime converts a calendar date into a UTC midnight time.
func ToTime(d *protolike.Date) time.Time {
	return time.Date(int(d.GetYear()), time.Month(d.GetMonth()), int(d.GetDay()), 0, 0, 0, 0, time.UTC)
}
