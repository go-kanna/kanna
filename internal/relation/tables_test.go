package relation_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/go-kanna/kanna/internal/diag"
	"github.com/go-kanna/kanna/internal/packages"
	"github.com/go-kanna/kanna/internal/pkgtest"
	"github.com/go-kanna/kanna/internal/relation"
	"github.com/go-kanna/kanna/internal/scan"
)

func tablesOf(t *testing.T, src string) (map[string]relation.TableFields, []diag.Diag) {
	t.Helper()

	pkg := pkgtest.LoadFileAs(t, "model", src)
	structs, ds := scan.Structs([]*packages.Package{pkg})
	if diag.HasErrors(ds) {
		t.Fatalf("scan: %s", diag.Format(ds))
	}
	return relation.Tables(structs)
}

func TestTablesClassifiesFields(t *testing.T) {
	t.Parallel()

	tables, ds := tablesOf(t, `package model

type helper struct{ N int }

//kanna:table
type User struct {
	ID        int64
	Email     string `+"`orm:\"email_address\"`"+`
	Secret    string `+"`orm:\"-\"`"+`
	Posts     []Post `+"`orm:\"has_many,foreign_key:user_id\"`"+`
	Scratch   []helper
	unexported string
}

//kanna:table
type Post struct {
	ID     int64
	UserID int64
	Title  string
	User   *User `+"`orm:\"belongs_to,foreign_key:user_id\"`"+`
}

type NotATable struct {
	ID int64
}
`)
	if diag.HasErrors(ds) {
		t.Fatalf("unexpected errors: %s", diag.Format(ds))
	}

	if len(tables) != 2 {
		t.Fatalf("tables = %v, want User and Post only", tables)
	}

	user := tables["User"]
	if want := []string{"ID", "Email"}; !slices.Equal(user.Columns, want) {
		t.Errorf("User.Columns = %v, want %v: orm:\"-\" and relation-shaped fields are not columns", user.Columns, want)
	}
	if want := []string{"Posts"}; !slices.Equal(user.Relations, want) {
		t.Errorf("User.Relations = %v, want %v", user.Relations, want)
	}

	post := tables["Post"]
	if want := []string{"ID", "UserID", "Title"}; !slices.Equal(post.Columns, want) {
		t.Errorf("Post.Columns = %v, want %v", post.Columns, want)
	}
	if want := []string{"User"}; !slices.Equal(post.Relations, want) {
		t.Errorf("Post.Relations = %v, want %v", post.Relations, want)
	}
}

func TestTablesReportsMalformedTags(t *testing.T) {
	t.Parallel()

	tables, ds := tablesOf(t, `package model

//kanna:table
type User struct {
	ID   int64
	Name string `+"`orm:\"name,unique\"`"+`
}
`)
	if !diag.HasErrors(ds) {
		t.Fatal("Tables() reported no error for an unknown option")
	}
	if got := diag.Format(ds); !strings.Contains(got, `unknown option "unique"`) {
		t.Errorf("diags = %q, want the unknown-option error", got)
	}
	if cols := tables["User"].Columns; slices.Contains(cols, "Name") {
		t.Errorf("Columns = %v: a field with a malformed tag must not classify", cols)
	}
}
