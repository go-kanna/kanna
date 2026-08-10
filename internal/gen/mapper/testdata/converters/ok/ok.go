// Package ok registers converters in all supported forms.
package ok

import (
	"strconv"
	"time"

	"github.com/go-kanna/kanna/mapper"
)

// UserID is a sample domain identifier.
type UserID int64

// Timestamp is a sample epoch-second value.
type Timestamp int64

func init() {
	mapper.Register(FormatUserID)
	mapper.Register[Timestamp, time.Time](ToTime)
	mapper.RegisterE(ParseUserID)
	mapper.RegisterE(strconv.Atoi)
}

// FormatUserID renders a UserID in decimal form.
func FormatUserID(id UserID) string {
	return strconv.FormatInt(int64(id), 10)
}

// ParseUserID parses a decimal UserID.
func ParseUserID(s string) (UserID, error) {
	n, err := strconv.ParseInt(s, 10, 64)
	return UserID(n), err
}

// ToTime converts epoch seconds into a UTC time.
func ToTime(ts Timestamp) time.Time {
	return time.Unix(int64(ts), 0).UTC()
}
