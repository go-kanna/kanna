// Command kanna-di generates type-safe dependency-injection constructors from
// container struct declarations.
package main

import (
	"os"

	"github.com/go-kanna/kanna/internal/gen/di"
	"github.com/go-kanna/kanna/internal/version"
)

func main() {
	os.Exit(di.NewCLI(version.String()).Run(os.Args[1:]))
}
