// Package unexported registers an unexported converter, which the generator
// rejects unless the output package is the same.
package unexported

import (
	"strconv"

	"github.com/go-kanna/kanna/mapper"
)

func init() {
	mapper.Register(format)
}

func format(v int) string {
	return strconv.Itoa(v)
}
