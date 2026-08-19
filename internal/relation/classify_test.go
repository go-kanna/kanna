package relation_test

import (
	"testing"

	"github.com/go-kanna/kanna/internal/ir"
	"github.com/go-kanna/kanna/internal/relation"
)

func structNamedOf(t *testing.T, src, name string) ir.Struct {
	t.Helper()

	for _, s := range scanOf(t, src) {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("no struct %s", name)
	return ir.Struct{}
}

// Writing any orm tag turns the name-based timestamp inference off; the
// explicit options turn it back on wherever the author says so.
func TestClassifyTableTimestampFacts(t *testing.T) {
	t.Parallel()

	s := structNamedOf(t, `package model

import "time"

//kanna:table
type User struct {
	ID        int64
	CreatedAt time.Time
	UpdatedAt time.Time `+"`orm:\"changed_at\"`"+`
	Touched   time.Time `+"`orm:\",updated_at\"`"+`
}
`, "User")

	c := relation.ClassifyTable(s)
	facts := make(map[string]relation.ColumnFacts, len(c.Fields))
	for _, cf := range c.Fields {
		if cf.Column != nil {
			facts[cf.Field.Name] = *cf.Column
		}
	}

	if f := facts["CreatedAt"]; !f.CreatedAt {
		t.Errorf("untagged CreatedAt = %+v, want the name-based inference on", f)
	}
	if f := facts["UpdatedAt"]; f.UpdatedAt || f.Name != "changed_at" {
		t.Errorf("tagged UpdatedAt = %+v, want inference off and the explicit column name", f)
	}
	if f := facts["Touched"]; !f.UpdatedAt {
		t.Errorf("orm:\",updated_at\" = %+v, want the explicit option on", f)
	}
}
