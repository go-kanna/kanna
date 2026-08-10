// Package method registers a method expression, which the generator rejects.
package method

import (
	"time"

	"github.com/go-kanna/kanna/mapper"
)

func init() {
	mapper.Register(time.Time.String)
}
