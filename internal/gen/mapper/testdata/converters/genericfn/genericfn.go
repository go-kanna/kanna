// Package genericfn registers generic converters, which the generator rejects.
package genericfn

import (
	"github.com/go-kanna/kanna/mapper"
)

func init() {
	mapper.Register(identity[int])
	mapper.Register[string, string](identity)
}

func identity[T any](v T) T {
	return v
}
