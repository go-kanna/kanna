// Package service holds the application services of the example.
package service

import (
	"fmt"

	"github.com/go-kanna/kanna/examples/di/infra"
)

// Env names the deployment environment.
//
// It is a named type rather than a bare string because kanna-di resolves by
// type: two dependencies that are both plain strings would be indistinguishable.
type Env string

// User registers accounts.
type User struct {
	db  *infra.DB
	env Env
}

// NewUser provides a User. The *infra.DB comes from another provider, while Env
// reaches it from the container's constructor arguments.
func NewUser(db *infra.DB, env Env) User {
	return User{db: db, env: env}
}

// Register records a new account.
func (u User) Register(name string) error {
	fmt.Printf("register %s on %s (%s)\n", name, u.db.DSN(), u.env)
	return nil
}

// Greeter greets by name.
type Greeter interface {
	Greet(name string) string
}

type greeter struct {
	env Env
}

// NewGreeter provides a Greeter. Env comes from the container's constructor
// arguments rather than from another provider.
func NewGreeter(env Env) Greeter {
	return greeter{env: env}
}

func (g greeter) Greet(name string) string {
	return fmt.Sprintf("Hello, %s! (%s)", name, g.env)
}

// Notifier delivers messages.
//
// Two providers below return one, so a container asking for a Notifier has to
// say which it means.
type Notifier interface {
	Notify(msg string) error
}

type logNotifier struct{}

// NewLogNotifier provides a Notifier that prints.
func NewLogNotifier() Notifier { return logNotifier{} }

func (logNotifier) Notify(msg string) error {
	fmt.Println("notify:", msg)
	return nil
}

type nullNotifier struct{}

// NewNullNotifier provides a Notifier that discards.
func NewNullNotifier() Notifier { return nullNotifier{} }

func (nullNotifier) Notify(string) error { return nil }
