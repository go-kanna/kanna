// Command kanna-mapper generates struct-to-struct mapping functions, wiring in
// the converters a project registers for the field types Go cannot convert on
// its own.
package main

import (
	"os"

	"github.com/go-kanna/kanna/internal/gen/mapper"
	"github.com/go-kanna/kanna/internal/version"
)

func main() {
	os.Exit(mapper.NewCLI(version.String()).Run(os.Args[1:]))
}
