// Package model holds the hand-written domain types.
package model

import (
	"time"

	"github.com/google/uuid"
)

// Employee is the domain-side aggregate.
type Employee struct {
	// Matched to the wire's Id without a tag: names that differ only in case
	// still find each other.
	ID uuid.UUID

	// The domain calls this FullName where the wire says Name, so the tag spells
	// out which field it pairs with. Without it the field would be reported as
	// unmapped rather than guessed at.
	FullName string `map:"Name"`

	HiredAt   time.Time
	Address   Address
	Nicknames []string

	// Local bookkeeping that never crosses the wire. Without the tag the
	// wire-to-domain direction fails: there is nothing on the wire to fill this
	// from, and the generator refuses to guess.
	SyncedAt time.Time `map:"-"`
}

// Address is a nested value object.
type Address struct {
	City   string
	Street string
}
