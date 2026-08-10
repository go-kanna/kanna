// Package dup registers two converters for the same type pair.
package dup

import (
	"strconv"

	"github.com/go-kanna/kanna/mapper"
)

func init() {
	mapper.Register(First)
	mapper.Register(Second)
}

// First renders an int in decimal form.
func First(v int) string {
	return strconv.Itoa(v)
}

// Second renders an int in decimal form.
func Second(v int) string {
	return strconv.Itoa(v)
}
