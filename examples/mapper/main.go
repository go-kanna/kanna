// Command mapper maps a domain value onto the wire types and back, showing that
// what kanna-mapper generates is ordinary Go you call directly.
//
// Regenerate with:
//
//	go generate ./...
package main

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/go-kanna/kanna/examples/mapper/gen/employeev1"
	"github.com/go-kanna/kanna/examples/mapper/mapper"
	"github.com/go-kanna/kanna/examples/mapper/model"
)

func main() {
	emp := model.Employee{
		ID: uuid.New(),
		// The domain name; the wire calls it Name, which the map tag pairs up.
		FullName:  "Alice",
		HiredAt:   time.Date(2024, time.April, 1, 9, 30, 0, 0, time.UTC),
		Address:   model.Address{City: "Tokyo", Street: "1-2-3"},
		Nicknames: []string{"al", "ali"},
		SyncedAt:  time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC),
	}

	// The domain types have no counterpart on the wire — a UUID becomes a
	// string, a time.Time becomes a calendar date — so both go through the
	// converters registered in lib/converters.
	wire := mapper.EmployeeToEmployeev1(emp)
	fmt.Printf("to wire:   id=%s hired=%d-%02d-%02d city=%s\n",
		wire.GetId(), wire.GetHiredAt().GetYear(), wire.GetHiredAt().GetMonth(), wire.GetHiredAt().GetDay(),
		wire.GetAddress().GetCity())

	// Coming back can fail, because parsing a UUID can, so the generated
	// function returns an error and says which field it came from.
	back, err := mapper.EmployeeFromEmployeev1(wire)
	if err != nil {
		panic(err)
	}
	fmt.Printf("from wire: id=%s name=%s hired=%s\n", back.ID, back.FullName, back.HiredAt.Format(time.DateOnly))

	// The wire side is nil-safe throughout: a nil message maps to the zero
	// value rather than panicking.
	empty, err := mapper.EmployeeFromEmployeev1(nil)
	if err != nil {
		panic(err)
	}
	fmt.Printf("from nil:  zero value: %t\n", empty.ID == uuid.Nil && empty.FullName == "")

	// SyncedAt is tagged map:"-", so it takes no part in either direction and
	// comes back zero however it was set.
	fmt.Printf("excluded:  SyncedAt dropped: %t\n", back.SyncedAt.IsZero())

	// A malformed id is reported with the field that produced it.
	if _, err := mapper.EmployeeFromEmployeev1(&employeev1.Employee{Id: "not-a-uuid"}); err != nil {
		fmt.Println("bad id:   ", err)
	}
}
