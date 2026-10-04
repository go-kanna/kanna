package app_test

import (
	"testing"

	"github.com/go-kanna/kanna/examples/di/app"
	"github.com/go-kanna/kanna/examples/di/app/infra"
	"github.com/go-kanna/kanna/examples/di/app/service"
)

// testEnv bundles what the tests of this package reach for. It is declared in a
// _test.go file, so kanna-di writes its constructor to di_gen_test.go, in this
// external test package, and nothing of it ships with the package.
//
// Two things differ from App in app.go. The embed field is named: the test
// keeps deps to look at the database handle, while its fields still serve as
// resolution sources. And application resolves against NewApp, a constructor
// generated in the same run.
//
//kanna:container name=newTestEnv
type testEnv struct {
	deps infra.Deps  `di:"embed"`
	_    service.Env `di:"arg"`

	application *app.App     `di:""`
	user        service.User `di:""`
}

// Everything arrives wired, so the test states only what it is about. The call
// to newTestEnv is also what the first run of kanna-di has to tolerate: the
// constructor does not exist until it has been generated.
func TestRegisterAndNotify(t *testing.T) {
	t.Parallel()

	env := newTestEnv(infra.MustNewDeps(), "test")

	if err := env.user.Register("alice"); err != nil {
		t.Fatal(err)
	}
	if err := env.application.Notifier.Notify("alice registered"); err != nil {
		t.Fatal(err)
	}
	if env.deps.DB.DSN() == "" {
		t.Error("the embedded deps are kept on the container")
	}
}
