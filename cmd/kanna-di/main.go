// Command kanna-di generates type-safe dependency-injection constructors from
// container struct declarations.
package main

import (
	"os"

	"github.com/go-kanna/kanna/internal/gen/di"
)

// version is set via -ldflags at build time. The default is "dev".
var version = "dev"

func main() {
	os.Exit(di.NewCLI(version).Run(os.Args[1:]))
}
