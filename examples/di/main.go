// Command di wires a small application with kanna-di, exercising the tags and
// directives it understands.
//
// Regenerate with:
//
//	go generate ./...
package main

import (
	"fmt"

	"github.com/go-kanna/kanna/examples/di/infra"
	"github.com/go-kanna/kanna/examples/di/service"
)

//go:generate go tool kanna-di ./...

// App is the application container.
//
// Its fields show most forms of the di tag:
//
//   - di:"embed"    takes a struct apart and offers its exported fields as
//     resolution sources — here everything Deps holds
//   - di:"arg"      declares a constructor parameter, named after its type
//   - di:""         resolves from whichever provider returns the field's type
//   - di:"with=..." resolves from a named provider, which is required here
//     because two providers return a service.Notifier
//
// One form is left out because it needs a conflict to be worth showing:
// di:"arg=name" spells the parameter name out, which is how you separate two
// arguments whose types would otherwise derive the same name.
//
//kanna:container name=NewApp
type App struct {
	_ infra.Deps  `di:"embed"`
	_ service.Env `di:"arg"`

	User     service.User     `di:""`
	Notifier service.Notifier `di:"with=service.NewLogNotifier"`
}

func main() {
	app := NewApp(infra.MustNewDeps(), "production")

	if err := app.User.Register("alice"); err != nil {
		panic(err)
	}
	if err := app.Notifier.Notify("alice registered"); err != nil {
		panic(err)
	}

	// A container may also present itself as an interface; see greeter.go.
	fmt.Println(NewGreeterService("staging").Greet("Bob"))
}
