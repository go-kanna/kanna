// Package app wires a small application with kanna-di, exercising the tags
// and directives it understands.
//
// The main function lives in cmd/demo, outside the packages the directive
// below scans. kanna-di needs the packages it loads to type-check, so keeping
// the callers of the generated constructors out of the scanned tree means a
// stale di_gen.go can always be deleted and regenerated instead of wedging
// the build.
//
// Regenerate with:
//
//	go generate ./...
package app

import (
	"github.com/go-kanna/kanna/examples/di/app/infra"
	"github.com/go-kanna/kanna/examples/di/app/service"
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
