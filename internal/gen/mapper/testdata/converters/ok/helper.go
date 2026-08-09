package ok

import (
	"time"

	"github.com/go-kanna/kanna/mapper"
)

// registerMore exercises registration outside init through an aliased
// import; the generator picks it up statically.
func registerMore() {
	mapper.Register(Truncate)
}

// Truncate converts a time into epoch seconds.
func Truncate(t time.Time) Timestamp {
	return Timestamp(t.Unix())
}
