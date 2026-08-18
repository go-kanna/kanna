package relation_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/go-kanna/kanna/internal/diag"
	"github.com/go-kanna/kanna/internal/relation"
)

func tableSetOf(t *testing.T, src string) (relation.TableSet, []diag.Diag) {
	t.Helper()

	return relation.Tables(scanOf(t, src))
}

func TestTablesClassifiesAndKeysByPackage(t *testing.T) {
	t.Parallel()

	set, ds := tableSetOf(t, `package model

//kanna:table
type User struct {
	ID     int64
	Email  string `+"`orm:\"email_address\"`"+`
	Secret string `+"`orm:\"-\"`"+`
	Posts  []Post `+"`orm:\"has_many,foreign_key:user_id\"`"+`
}

//kanna:table
type Post struct {
	ID     int64
	UserID int64
	User   *User `+"`orm:\"belongs_to,foreign_key:user_id\"`"+`
}

type NotATable struct{ ID int64 }
`)
	if diag.HasErrors(ds) {
		t.Fatalf("unexpected errors: %s", diag.Format(ds))
	}

	user, ok := set["model.User"]
	if !ok {
		t.Fatalf("set = %v, want a model.User entry", set)
	}
	if want := []string{"ID", "Email"}; !slices.Equal(user.Columns, want) {
		t.Errorf("User.Columns = %v, want %v", user.Columns, want)
	}
	if want := []string{"Posts"}; !slices.Equal(user.Relations, want) {
		t.Errorf("User.Relations = %v, want %v", user.Relations, want)
	}
	if user.Broken {
		t.Error("User marked broken without a malformed tag")
	}
	if _, ok := set["model.NotATable"]; ok {
		t.Error("a struct without the directive classified as a table")
	}
}

func TestTablesMarksBrokenAndReportsTags(t *testing.T) {
	t.Parallel()

	set, ds := tableSetOf(t, `package model

//kanna:table
type User struct {
	ID   int64
	Name string `+"`orm:\"name,unique\"`"+`
}
`)
	if got := diag.Format(ds); !strings.Contains(got, `unknown option "unique"`) {
		t.Errorf("diags = %q, want the unknown-option error", got)
	}
	user := set["model.User"]
	if !user.Broken {
		t.Error("a table with a malformed tag must be marked broken")
	}
	if slices.Contains(user.Columns, "Name") {
		t.Errorf("Columns = %v: a field with a malformed tag must not classify", user.Columns)
	}
}
