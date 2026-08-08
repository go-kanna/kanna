package main

import "github.com/go-kanna/kanna/examples/di/service"

// greeterApp is a container that presents itself as a service.Greeter instead
// of as *greeterApp, by implementing the interface and delegating.
//
// The di:"returns" tag does two things: it stores the resolved value in the
// field, and it declares that value's type as the constructor's return type.
// Writing //kanna:container returns=service.Greeter has the same effect without
// spending a field on it.
//
// Because the constructor is declared to return service.Greeter, *greeterApp
// must satisfy that interface — the Greet method below is what makes it so.
//
//kanna:container name=NewGreeterService
type greeterApp struct {
	_ service.Env `di:"arg"`

	greeter service.Greeter `di:"returns"`
}

// Greet delegates to the wired implementation.
func (a *greeterApp) Greet(name string) string {
	return a.greeter.Greet(name)
}
