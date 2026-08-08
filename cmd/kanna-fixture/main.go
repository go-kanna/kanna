// Command kanna-fixture generates plain fixture functions from the structs a
// source package declares.
package main

import (
	"os"

	"github.com/go-kanna/kanna/internal/gen/fixture"
	"github.com/go-kanna/kanna/internal/version"
)

func main() {
	os.Exit(fixture.NewCLI(version.String()).Run(os.Args[1:]))
}
