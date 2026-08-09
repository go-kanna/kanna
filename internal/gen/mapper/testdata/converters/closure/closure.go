// Package closure registers a function literal, which the generator rejects.
package closure

import (
	"strconv"

	"github.com/go-kanna/kanna/mapper"
)

func init() {
	mapper.Register(func(v int) string { return strconv.Itoa(v) })
}
