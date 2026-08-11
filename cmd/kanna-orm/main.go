// Command kanna-orm generates type-safe query code from the annotated model
// structs a source package declares.
package main

import (
	"os"

	"github.com/go-kanna/kanna/internal/gen/orm"
	"github.com/go-kanna/kanna/internal/version"
)

func main() {
	os.Exit(orm.NewCLI(version.String()).Run(os.Args[1:]))
}
