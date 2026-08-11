// Command kanna-i18n generates typed message constructors from a directory of
// locale files, with the translations themselves compiled into the output.
package main

import (
	"os"

	"github.com/go-kanna/kanna/internal/gen/i18n"
	"github.com/go-kanna/kanna/internal/version"
)

func main() {
	os.Exit(i18n.NewCLI(version.String()).Run(os.Args[1:]))
}
