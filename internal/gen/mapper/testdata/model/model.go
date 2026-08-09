// Package model holds hand-written domain types for resolver tests.
package model

import "time"

// UserID is a domain identifier requiring a converter to string.
type UserID int64

// Tag is a named string used to exercise element-wise slice conversion.
type Tag string

// Employee exercises the full matching and conversion rule set.
type Employee struct {
	ID           UserID `map:"Id"`
	EmployeeName string `map:"Name"`
	Age          int32
	HiredAt      time.Time
	Address      Address
	Tags         []Tag
	Subordinates []Employee
	Note         string
	Secret       string `map:"-"`
	CreatedAt    time.Time
}

// Address is a nested pair target.
type Address struct {
	City   string
	Street string
}

// Base is embedded into WithBase to exercise promoted field reads.
type Base struct {
	Code string
}

// WithBase exercises promoted source fields and embedded destination
// fields.
type WithBase struct {
	Base
	Name string
}
