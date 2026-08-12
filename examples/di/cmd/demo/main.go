// Command demo runs the application wired in package app.
//
// It deliberately lives outside the packages kanna-di scans: this is the only
// place that calls the generated constructors, so the scanned packages keep
// type-checking even while di_gen.go is stale or deleted.
package main

import (
	"fmt"

	"github.com/go-kanna/kanna/examples/di/app"
	"github.com/go-kanna/kanna/examples/di/app/infra"
)

func main() {
	a := app.NewApp(infra.MustNewDeps(), "production")

	if err := a.User.Register("alice"); err != nil {
		panic(err)
	}
	if err := a.Notifier.Notify("alice registered"); err != nil {
		panic(err)
	}

	// A container may also present itself as an interface; see app/greeter.go.
	fmt.Println(app.NewGreeterService("staging").Greet("Bob"))
}
