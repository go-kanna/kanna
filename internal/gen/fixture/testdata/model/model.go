package model

import "time"

// Nickname is an alias; fields of this type infer as the aliased type.
type Nickname = string

// Status is a named basic type; untagged fields of this type stay zero
// values, tagged ones convert through the named type.
type Status string

// Level is a named basic type exercised through a parameterized tag.
type Level int

// User exercises tags, heuristics, type-based rules, and references.
type User struct {
	ID        int64
	Name      string
	Email     string
	Age       int    `fake:"{number:18,65}"`
	Code      string `fake:"###-####"`
	Bio       string `fake:"skip"`
	Nick      Nickname
	Height    float64
	Active    bool
	Note      *string
	Tags      []string
	Status    Status
	Kind      Status `fake:"{word}"`
	Level     Level  `fake:"{number:1,5}"`
	CreatedAt time.Time
	Profile   Profile
	secret    string
}

// Profile is referenced by User and generated on its own.
type Profile struct {
	FirstName string
	LastName  string
	URL       string
}

// Empty has no inferable fields.
type Empty struct {
	Ptr *int
}

// Ignored is excluded by the directive.
//
//kanna:ignore
type Ignored struct {
	Token string
}

// Spaced writes the directive with a space, which is not a directive at all, so
// it is still generated.
//
// kanna:ignore
type Spaced struct {
	Token string
}

// Pair is generic and skipped with a warning.
type Pair[T any] struct {
	A T
	B T
}
