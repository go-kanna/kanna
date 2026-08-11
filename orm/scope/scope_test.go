package scope_test

import (
	"testing"

	"github.com/go-kanna/kanna/orm/scope"
)

// mockApplier records calls from Scope.Apply for assertions.
type mockApplier struct {
	wheres              []appliedWhere
	orderBys            []string
	selects             []string
	joins               []string
	leftJoins           []string
	preloads            []string
	limit               *int
	offset              *int
	forUpdate           bool
	forUpdateSkipLocked bool
}

type appliedWhere struct {
	clause string
	args   []any
}

func (m *mockApplier) ApplyWhere(clause string, args []any) {
	m.wheres = append(m.wheres, appliedWhere{clause, args})
}
func (m *mockApplier) ApplyOrderBy(clause string) { m.orderBys = append(m.orderBys, clause) }
func (m *mockApplier) ApplyLimit(n int)           { m.limit = &n }
func (m *mockApplier) ApplyOffset(n int)          { m.offset = &n }
func (m *mockApplier) ApplySelect(columns string) { m.selects = append(m.selects, columns) }
func (m *mockApplier) ApplyJoin(name string)      { m.joins = append(m.joins, name) }
func (m *mockApplier) ApplyLeftJoin(name string)  { m.leftJoins = append(m.leftJoins, name) }
func (m *mockApplier) ApplyPreload(name string)   { m.preloads = append(m.preloads, name) }
func (m *mockApplier) ApplyForUpdate()            { m.forUpdate = true }
func (m *mockApplier) ApplyForUpdateSkipLocked()  { m.forUpdateSkipLocked = true }

func TestWhere(t *testing.T) {
	t.Parallel()

	m := &mockApplier{}
	scope.Where("age > ?", 18).Apply(m)

	if len(m.wheres) != 1 {
		t.Fatalf("expected 1 where, got %d", len(m.wheres))
	}
	if m.wheres[0].clause != "age > ?" {
		t.Errorf("clause = %q, want %q", m.wheres[0].clause, "age > ?")
	}
	if len(m.wheres[0].args) != 1 || m.wheres[0].args[0] != 18 {
		t.Errorf("args = %v, want [18]", m.wheres[0].args)
	}
}

func TestWhereMultipleArgs(t *testing.T) {
	t.Parallel()

	m := &mockApplier{}
	scope.Where("name = ? AND role = ?", "alice", "admin").Apply(m)

	if len(m.wheres) != 1 {
		t.Fatalf("expected 1 where, got %d", len(m.wheres))
	}
	if m.wheres[0].clause != "name = ? AND role = ?" {
		t.Errorf("clause = %q", m.wheres[0].clause)
	}
	if len(m.wheres[0].args) != 2 {
		t.Errorf("args = %v, want 2 args", m.wheres[0].args)
	}
}

func TestOrderBy(t *testing.T) {
	t.Parallel()

	m := &mockApplier{}
	scope.OrderBy("created_at DESC").Apply(m)

	if len(m.orderBys) != 1 || m.orderBys[0] != "created_at DESC" {
		t.Errorf("orderBys = %v, want [created_at DESC]", m.orderBys)
	}
}

func TestLimit(t *testing.T) {
	t.Parallel()

	m := &mockApplier{}
	scope.Limit(10).Apply(m)

	if m.limit == nil || *m.limit != 10 {
		t.Errorf("limit = %v, want 10", m.limit)
	}
}

func TestOffset(t *testing.T) {
	t.Parallel()

	m := &mockApplier{}
	scope.Offset(20).Apply(m)

	if m.offset == nil || *m.offset != 20 {
		t.Errorf("offset = %v, want 20", m.offset)
	}
}

func TestSelect(t *testing.T) {
	t.Parallel()

	m := &mockApplier{}
	scope.Select("id", "name", "email").Apply(m)

	if len(m.selects) != 1 || m.selects[0] != "id, name, email" {
		t.Errorf("selects = %v, want [id, name, email]", m.selects)
	}
}

func TestIn(t *testing.T) {
	t.Parallel()

	m := &mockApplier{}
	scope.In("id", []int{1, 2, 3}).Apply(m)

	if len(m.wheres) != 1 {
		t.Fatalf("expected 1 where, got %d", len(m.wheres))
	}
	if m.wheres[0].clause != "id IN (?, ?, ?)" {
		t.Errorf("clause = %q, want %q", m.wheres[0].clause, "id IN (?, ?, ?)")
	}
	if len(m.wheres[0].args) != 3 {
		t.Fatalf("args len = %d, want 3", len(m.wheres[0].args))
	}
	for i, want := range []int{1, 2, 3} {
		if m.wheres[0].args[i] != want {
			t.Errorf("args[%d] = %v, want %d", i, m.wheres[0].args[i], want)
		}
	}
}

func TestInEmpty(t *testing.T) {
	t.Parallel()

	m := &mockApplier{}
	scope.In("id", []int{}).Apply(m)

	if len(m.wheres) != 1 {
		t.Fatalf("expected 1 where, got %d", len(m.wheres))
	}
	if m.wheres[0].clause != "1 = 0" {
		t.Errorf("clause = %q, want %q", m.wheres[0].clause, "1 = 0")
	}
}

func TestInStrings(t *testing.T) {
	t.Parallel()

	m := &mockApplier{}
	scope.In("role", []string{"admin", "editor"}).Apply(m)

	if m.wheres[0].clause != "role IN (?, ?)" {
		t.Errorf("clause = %q", m.wheres[0].clause)
	}
	if m.wheres[0].args[0] != "admin" || m.wheres[0].args[1] != "editor" {
		t.Errorf("args = %v", m.wheres[0].args)
	}
}

func TestScopesAppend(t *testing.T) {
	t.Parallel()

	s1 := scope.Scopes{scope.Where("a = ?", 1)}
	s2 := s1.Append(scope.Where("b = ?", 2), scope.Limit(10))

	if len(s1) != 1 {
		t.Errorf("original modified: len = %d, want 1", len(s1))
	}
	if len(s2) != 3 {
		t.Errorf("appended len = %d, want 3", len(s2))
	}
}

func TestJoin(t *testing.T) {
	t.Parallel()

	m := &mockApplier{}
	scope.Join("Posts").Apply(m)

	if len(m.joins) != 1 || m.joins[0] != "Posts" {
		t.Errorf("joins = %v, want [Posts]", m.joins)
	}
}

func TestLeftJoin(t *testing.T) {
	t.Parallel()

	m := &mockApplier{}
	scope.LeftJoin("Posts").Apply(m)

	if len(m.leftJoins) != 1 || m.leftJoins[0] != "Posts" {
		t.Errorf("leftJoins = %v, want [Posts]", m.leftJoins)
	}
}

func TestPreload(t *testing.T) {
	t.Parallel()

	m := &mockApplier{}
	scope.Preload("Posts").Apply(m)

	if len(m.preloads) != 1 || m.preloads[0] != "Posts" {
		t.Errorf("preloads = %v, want [Posts]", m.preloads)
	}
}

func TestForUpdate(t *testing.T) {
	t.Parallel()

	m := &mockApplier{}
	scope.ForUpdate().Apply(m)

	if !m.forUpdate {
		t.Error("forUpdate = false, want true")
	}
}

func TestScopesAppendDoesNotMutate(t *testing.T) {
	t.Parallel()

	original := scope.Scopes{scope.Where("x = ?", 1)}
	_ = original.Append(scope.Where("y = ?", 2))

	if len(original) != 1 {
		t.Fatalf("original mutated: len = %d", len(original))
	}
}
