package orm_test

import (
	"strings"
	"testing"

	"github.com/go-kanna/kanna/internal/diag"
	"github.com/go-kanna/kanna/internal/gen/orm"
	"github.com/go-kanna/kanna/internal/packages"
	"github.com/go-kanna/kanna/internal/pkgtest"
	"github.com/go-kanna/kanna/internal/scan"
)

func planOf(t *testing.T, src string) ([]orm.Table, []diag.Diag) {
	t.Helper()

	pkg := pkgtest.LoadFileAs(t, "model", src)
	structs, ds := scan.Structs([]*packages.Package{pkg})
	if diag.HasErrors(ds) {
		t.Fatalf("scan: %s", diag.Format(ds))
	}
	return orm.Tables(structs)
}

func mustPlan(t *testing.T, src string) []orm.Table {
	t.Helper()

	tables, ds := planOf(t, src)
	if diag.HasErrors(ds) {
		t.Fatalf("unexpected errors: %s", diag.Format(ds))
	}
	return tables
}

func wantError(t *testing.T, src, substr string) {
	t.Helper()

	tables, ds := planOf(t, src)
	if !diag.HasErrors(ds) {
		t.Fatalf("expected error containing %q, got tables %+v", substr, tables)
	}
	if got := diag.Format(ds); !strings.Contains(got, substr) {
		t.Errorf("diags = %q, want substring %q", got, substr)
	}
}

const exampleModel = `package model

import "time"

//kanna:table
type User struct {
	ID        int
	Name      string
	Email     string
	CreatedAt time.Time
	Posts     []Post   ` + "`" + `orm:"has_many,foreign_key:user_id"` + "`" + `
	Profile   *Profile ` + "`" + `orm:"has_one,foreign_key:user_id"` + "`" + `
	Tags      []Tag    ` + "`" + `orm:"many_to_many,join_table:user_tags,foreign_key:user_id,references:tag_id"` + "`" + `
}

//kanna:table
type Post struct {
	ID     int
	UserID int
	Title  string
	User   *User ` + "`" + `orm:"belongs_to,foreign_key:user_id"` + "`" + `
}

//kanna:table
type Profile struct {
	ID     int
	UserID int
	Bio    string
}

//kanna:table
type Tag struct {
	ID   int
	Name string
}
`

func TestTablesExampleModel(t *testing.T) {
	t.Parallel()

	tables := mustPlan(t, exampleModel)
	if len(tables) != 4 {
		t.Fatalf("tables = %d, want 4", len(tables))
	}

	user := tables[0]
	if user.Name != "User" || user.TableName != "users" {
		t.Errorf("user = %s/%s, want User/users", user.Name, user.TableName)
	}
	if pk := user.PK(); pk.Name != "ID" || pk.Column != "id" || pk.GoType != "int" {
		t.Errorf("pk = %+v", pk)
	}

	cols := make([]string, 0, len(user.Fields))
	for _, f := range user.Fields {
		cols = append(cols, f.Column)
	}
	if got, want := strings.Join(cols, ","), "id,name,email,created_at"; got != want {
		t.Errorf("columns = %s, want %s", got, want)
	}
	if !user.Fields[3].CreatedAt {
		t.Error("CreatedAt field not marked as timestamp")
	}
	if user.Fields[3].GoType != "time.Time" {
		t.Errorf("CreatedAt GoType = %s", user.Fields[3].GoType)
	}

	if len(user.Relations) != 3 {
		t.Fatalf("relations = %+v, want 3", user.Relations)
	}
	posts, profile, tags := user.Relations[0], user.Relations[1], user.Relations[2]
	if posts.Kind != "has_many" || posts.TargetType != "Post" || posts.ForeignKey != "user_id" || posts.IsPointer {
		t.Errorf("posts = %+v", posts)
	}
	if profile.Kind != "has_one" || profile.TargetType != "Profile" || !profile.IsPointer {
		t.Errorf("profile = %+v", profile)
	}
	if tags.Kind != "many_to_many" || tags.JoinTable != "user_tags" || tags.References != "tag_id" {
		t.Errorf("tags = %+v", tags)
	}

	post := tables[1]
	if len(post.Relations) != 1 || post.Relations[0].Kind != "belongs_to" {
		t.Errorf("post relations = %+v", post.Relations)
	}
	if len(post.Fields) != 3 {
		t.Errorf("post fields = %+v, want id, user_id, title", post.Fields)
	}
}

func TestTablesOptIn(t *testing.T) {
	t.Parallel()

	tables := mustPlan(t, `package model

//kanna:table
type User struct{ ID int }

type Helper struct{ ID int }
`)
	if len(tables) != 1 || tables[0].Name != "User" {
		t.Fatalf("tables = %+v, want just User", tables)
	}
}

func TestTablesWarnsOnTagsWithoutDirective(t *testing.T) {
	t.Parallel()

	_, ds := planOf(t, `package model

type User struct {
	ID   int
	Name string `+"`"+`orm:"full_name"`+"`"+`
}
`)
	if diag.HasErrors(ds) {
		t.Fatalf("unexpected errors: %s", diag.Format(ds))
	}
	if got := diag.Format(ds); !strings.Contains(got, "no //kanna:table directive") {
		t.Errorf("diags = %q, want opt-in warning", got)
	}
}

func TestTablesNameOverride(t *testing.T) {
	t.Parallel()

	tables := mustPlan(t, `package model

//kanna:table name=people
type Person struct{ ID int }
`)
	if tables[0].TableName != "people" {
		t.Errorf("table = %s, want people", tables[0].TableName)
	}
}

func TestTablesColumnRename(t *testing.T) {
	t.Parallel()

	tables := mustPlan(t, `package model

//kanna:table
type User struct {
	ID    int
	Email string `+"`"+`orm:"email_address"`+"`"+`
}
`)
	if tables[0].Fields[1].Column != "email_address" {
		t.Errorf("column = %s", tables[0].Fields[1].Column)
	}
}

func TestTablesExplicitPrimaryKeyWins(t *testing.T) {
	t.Parallel()

	tables := mustPlan(t, `package model

//kanna:table
type Session struct {
	Token string `+"`"+`orm:",primary_key"`+"`"+`
	Data  string
}
`)
	if pk := tables[0].PK(); pk.Name != "Token" {
		t.Errorf("pk = %+v, want Token", pk)
	}
}

func TestTablesSkipsUntaggedRelationShapes(t *testing.T) {
	t.Parallel()

	tables := mustPlan(t, `package model

import "time"

//kanna:table
type User struct {
	ID       int
	Note     *string
	SeenAt   *time.Time
	Raw      []byte
	Friends  []User
	Shadow   *User
	Ignored  string `+"`"+`orm:"-"`+"`"+`
}
`)
	cols := make([]string, 0, len(tables[0].Fields))
	for _, f := range tables[0].Fields {
		cols = append(cols, f.Column)
	}
	if got, want := strings.Join(cols, ","), "id,note,seen_at,raw"; got != want {
		t.Errorf("columns = %s, want %s", got, want)
	}
	if len(tables[0].Relations) != 0 {
		t.Errorf("relations = %+v, want none", tables[0].Relations)
	}
}

func TestTablesEmbeddedWarns(t *testing.T) {
	t.Parallel()

	_, ds := planOf(t, `package model

type Base struct{ ID int }

//kanna:table
type User struct {
	Base
	Key string `+"`"+`orm:",primary_key"`+"`"+`
}
`)
	if got := diag.Format(ds); !strings.Contains(got, "embedded field Base is ignored") {
		t.Errorf("diags = %q, want embedded warning", got)
	}
}

func TestTablesErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		src     string
		wantErr string
	}{
		{
			"no primary key",
			`package model

//kanna:table
type Log struct{ Message string }
`,
			"has no primary key",
		},
		{
			"two explicit primary keys",
			`package model

//kanna:table
type Pair struct {
	A string ` + "`" + `orm:",primary_key"` + "`" + `
	B string ` + "`" + `orm:",primary_key"` + "`" + `
}
`,
			"more than one primary_key",
		},
		{
			"unknown option",
			`package model

//kanna:table
type User struct {
	ID    int
	Email string ` + "`" + `orm:"email,unique"` + "`" + `
}
`,
			`unknown option "unique"`,
		},
		{
			"has_many on non-slice",
			`package model

//kanna:table
type User struct {
	ID   int
	Post *Post ` + "`" + `orm:"has_many,foreign_key:user_id"` + "`" + `
}

//kanna:table
type Post struct {
	ID     int
	UserID int
}
`,
			"must be a slice",
		},
		{
			"belongs_to foreign key missing",
			`package model

//kanna:table
type Post struct {
	ID   int
	User *User ` + "`" + `orm:"belongs_to,foreign_key:user_id"` + "`" + `
}

//kanna:table
type User struct{ ID int }
`,
			`foreign_key "user_id" is not a column of Post`,
		},
		{
			"has_many foreign key missing on target",
			`package model

//kanna:table
type User struct {
	ID    int
	Posts []Post ` + "`" + `orm:"has_many,foreign_key:user_id"` + "`" + `
}

//kanna:table
type Post struct{ ID int }
`,
			`foreign_key "user_id" is not a column of Post`,
		},
		{
			"relation target not a table",
			`package model

//kanna:table
type User struct {
	ID    int
	Posts []Post ` + "`" + `orm:"has_many,foreign_key:user_id"` + "`" + `
}

type Post struct {
	ID     int
	UserID int
}
`,
			"generates no queries",
		},
		{
			"duplicate table name",
			`package model

//kanna:table name=users
type User struct{ ID int }

//kanna:table name=users
type Account struct{ ID int }
`,
			`table "users" is already generated`,
		},
		{
			"duplicate column",
			`package model

//kanna:table
type User struct {
	ID   int
	Name string ` + "`" + `orm:"id"` + "`" + `
}
`,
			`column "id" is already used`,
		},
		{
			"timestamp with wrong type",
			`package model

//kanna:table
type User struct {
	ID        int
	CreatedAt string
}
`,
			"not time.Time",
		},
		{
			"generic struct",
			`package model

//kanna:table
type Box[T any] struct{ ID int }
`,
			"is generic",
		},
		{
			"bad directive argument",
			`package model

//kanna:table people
type Person struct{ ID int }
`,
			"takes no argument or name=<table>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			wantError(t, tt.src, tt.wantErr)
		})
	}
}

func TestTablesRejectsUnexportedStruct(t *testing.T) {
	t.Parallel()

	wantError(t, `package model

//kanna:table
type user struct{ ID int }
`, "user is unexported")
}

func TestTablesRejectsExtraDirectiveTokens(t *testing.T) {
	t.Parallel()

	wantError(t, `package model

//kanna:table name=my people
type Person struct{ ID int }
`, "takes no argument or name=<table>")
}

func TestTablesResolvesAliasedRelationTarget(t *testing.T) {
	t.Parallel()

	tables := mustPlan(t, `package model

//kanna:table
type User struct {
	ID    int
	Posts []P `+"`"+`orm:"has_many,foreign_key:user_id"`+"`"+`
}

type P = Post

//kanna:table
type Post struct {
	ID     int
	UserID int
}
`)
	if got := tables[0].Relations[0].TargetType; got != "Post" {
		t.Errorf("TargetType = %s, want Post (through the alias)", got)
	}
}

func TestTablesTimestampOptOut(t *testing.T) {
	t.Parallel()

	tables := mustPlan(t, `package model

//kanna:table
type Event struct {
	ID        int
	CreatedAt string `+"`"+`orm:"created_at_str"`+"`"+`
}
`)
	f := tables[0].Fields[1]
	if f.CreatedAt || f.Column != "created_at_str" {
		t.Errorf("field = %+v, want a plain column", f)
	}
}

func TestTablesUntaggedShapes(t *testing.T) {
	t.Parallel()

	tables := mustPlan(t, `package model

import (
	"database/sql"
	"time"
)

//kanna:table
type Entry struct {
	ID     int
	Times  []time.Time    // struct slice, but a column type: kept
	Nick   sql.NullString // sql.Scanner: kept
	Stamps []Entry        // same-package struct slice: relation shape, dropped
}
`)
	cols := make([]string, 0, len(tables[0].Fields))
	for _, f := range tables[0].Fields {
		cols = append(cols, f.Column)
	}
	if got, want := strings.Join(cols, ","), "id,times,nick"; got != want {
		t.Errorf("columns = %s, want %s", got, want)
	}
}

func TestTablesUntaggedCrossPackagePointerIsNotAColumn(t *testing.T) {
	t.Parallel()

	tables := mustPlan(t, `package model

import "go/token"

//kanna:table
type Entry struct {
	ID  int
	Ref *token.FileSet // struct pointer without a tag: relation shape, dropped
}
`)
	if len(tables[0].Fields) != 1 {
		t.Errorf("fields = %+v, want only id", tables[0].Fields)
	}
}
