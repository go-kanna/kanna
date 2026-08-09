// Package model holds the hand-written domain types.
package model

import (
	"time"

	"github.com/google/uuid"
)

// Employee is the domain-side aggregate.
type Employee struct {
	ID        uuid.UUID
	Name      string
	HiredAt   time.Time
	Address   Address
	Nicknames []string
}

// Address is a nested value object.
type Address struct {
	City   string
	Street string
}
