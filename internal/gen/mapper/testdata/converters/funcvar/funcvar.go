// Package funcvar registers a variable of function type, which the generator
// rejects.
package funcvar

import (
	"strconv"

	"github.com/go-kanna/kanna/mapper"
)

// Format is a converter held in a variable.
var Format = strconv.Itoa

func init() {
	mapper.Register(Format)
}
