// Package model holds the structs the fixtures are generated from. It knows
// nothing about kanna beyond the occasional tag or directive.
package model

import (
	"time"

	"github.com/google/uuid"
)

// Status is a named string. Untagged fields of a named basic type stay at their
// zero value, because the generator has no way to know which values are valid;
// a tag opts back in and converts through the type.
type Status string

const (
	StatusDraft     Status = "draft"
	StatusPublished Status = "published"
)

// User shows the three inference rules that matter most: the field name, the
// field type, and an explicit tag overriding either.
type User struct {
	ID    int64
	Name  string // filled from the name heuristic, not the type
	Email string // likewise
	Age   int    `fake:"{number:18,65}"` // a tag narrows the range

	// A pointer is left nil: it usually means "absent", and inventing a value
	// would erase that distinction.
	Nickname *string

	CreatedAt time.Time // any *At field of this type gets a date
}

// Article references User and carries a UUID, both of which need more than a
// basic-type rule.
type Article struct {
	// A uuid.UUID is an array underneath, so the type rule alone would leave it
	// zero. It is filled through gofakeit and parsed back.
	ID uuid.UUID

	Slug   string `fake:"???-####"` // an unknown template goes through gofakeit.Generate
	Title  string
	Status Status `fake:"{word}"`

	// A value field whose type is generated too calls that fixture.
	Author User

	PublishedAt time.Time
}

// Internal is excluded from generation.
//
//kanna:ignore
type Internal struct {
	Token string
}

// The structs below carry orm relations, which is what turns fixtures into
// graphs: a //kanna:table struct whose belongs_to keys cannot be NULL gets a
// New<Name>Graph constructor bundling everything it needs to exist.

//kanna:table
type Company struct {
	ID   int64
	Name string
}

//kanna:table
type Department struct {
	ID        int64
	CompanyID int64
	Company   *Company `orm:"belongs_to,foreign_key:company_id"`
	Name      string
}

//kanna:table
type Employee struct {
	ID           int64
	DepartmentID int64
	Department   *Department `orm:"belongs_to,foreign_key:department_id"`

	// An optional relation — a nullable foreign key — stays out of the graph.
	ManagerID *int64
	Manager   *Employee `orm:"belongs_to,foreign_key:manager_id"`

	Name string
}
