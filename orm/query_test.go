package orm_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/go-kanna/kanna/orm"
	"github.com/go-kanna/kanna/orm/scope"
)

type testUser struct {
	ID   int
	Name string
}

var testUserColumns = []string{"id", "name"}

func scanTestUser(_ *sql.Rows) (testUser, error) {
	return testUser{}, nil
}

func scanTestUserRows(rows *sql.Rows) (testUser, error) {
	var u testUser
	err := rows.Scan(&u.ID, &u.Name)
	return u, err //nolint:wrapcheck // pass through
}

func newTestScanQuery(tq *orm.TestQuerier) *orm.Query[testUser] {
	return orm.NewQuery[testUser](
		tq, "users", testUserColumns, "id", scanTestUserRows, testUserColValPairs, setTestUserPK)
}

func testUserColValPairs(u *testUser, includesPK bool) ([]string, []any) {
	if includesPK {
		return []string{"id", "name"}, []any{u.ID, u.Name}
	}
	return []string{"name"}, []any{u.Name}
}

func setTestUserPK(u *testUser, id int64) {
	u.ID = int(id)
}

func newTestQuery(tq *orm.TestQuerier) *orm.Query[testUser] {
	return orm.NewQuery[testUser](tq, "users", testUserColumns, "id", scanTestUser, testUserColValPairs, setTestUserPK)
}

// --- SELECT (MySQL) ---

func TestBuildSelectAll(t *testing.T) {
	t.Parallel()

	tq := orm.NewTestQuerier(orm.MySQL)
	q := newTestQuery(tq)

	_, _ = q.All(t.Context())

	got := tq.LastQuery()
	want := "SELECT `id`, `name` FROM `users`"
	if got.SQL != want {
		t.Errorf("SQL = %q, want %q", got.SQL, want)
	}
}

func TestBuildSelectWhere(t *testing.T) {
	t.Parallel()

	tq := orm.NewTestQuerier(orm.MySQL)
	q := newTestQuery(tq)

	_, _ = q.Where("name = ?", "alice").All(t.Context())

	got := tq.LastQuery()
	want := "SELECT `id`, `name` FROM `users` WHERE name = ?"
	if got.SQL != want {
		t.Errorf("SQL = %q, want %q", got.SQL, want)
	}
	if len(got.Args) != 1 || got.Args[0] != "alice" {
		t.Errorf("Args = %v", got.Args)
	}
}

func TestBuildSelectMultipleWhere(t *testing.T) {
	t.Parallel()

	tq := orm.NewTestQuerier(orm.MySQL)
	q := newTestQuery(tq)

	_, _ = q.Where("name = ?", "alice").Where("id > ?", 10).All(t.Context())

	got := tq.LastQuery()
	want := "SELECT `id`, `name` FROM `users` WHERE name = ? AND id > ?"
	if got.SQL != want {
		t.Errorf("SQL = %q, want %q", got.SQL, want)
	}
	if len(got.Args) != 2 {
		t.Errorf("Args = %v, want 2 args", got.Args)
	}
}

func TestBuildSelectOrderBy(t *testing.T) {
	t.Parallel()

	tq := orm.NewTestQuerier(orm.MySQL)
	q := newTestQuery(tq)

	_, _ = q.OrderBy("name ASC").All(t.Context())

	got := tq.LastQuery()
	want := "SELECT `id`, `name` FROM `users` ORDER BY name ASC"
	if got.SQL != want {
		t.Errorf("SQL = %q, want %q", got.SQL, want)
	}
}

func TestBuildSelectLimitOffset(t *testing.T) {
	t.Parallel()

	tq := orm.NewTestQuerier(orm.MySQL)
	q := newTestQuery(tq)

	_, _ = q.Limit(10).Offset(20).All(t.Context())

	got := tq.LastQuery()
	want := "SELECT `id`, `name` FROM `users` LIMIT 10 OFFSET 20"
	if got.SQL != want {
		t.Errorf("SQL = %q, want %q", got.SQL, want)
	}
}

func TestBuildSelectCustomColumns(t *testing.T) {
	t.Parallel()

	tq := orm.NewTestQuerier(orm.MySQL)
	q := newTestQuery(tq)

	_, _ = q.Select("id").All(t.Context())

	got := tq.LastQuery()
	want := "SELECT id FROM `users`"
	if got.SQL != want {
		t.Errorf("SQL = %q, want %q", got.SQL, want)
	}
}

func TestBuildSelectFull(t *testing.T) {
	t.Parallel()

	tq := orm.NewTestQuerier(orm.MySQL)
	q := newTestQuery(tq)

	_, _ = q.
		Where("name = ?", "alice").
		OrderBy("id DESC").
		Limit(5).
		Offset(10).
		All(t.Context())

	got := tq.LastQuery()
	want := "SELECT `id`, `name` FROM `users` WHERE name = ? ORDER BY id DESC LIMIT 5 OFFSET 10"
	if got.SQL != want {
		t.Errorf("SQL = %q, want %q", got.SQL, want)
	}
}

// --- Scopes ---

func TestBuildSelectWithScopes(t *testing.T) {
	t.Parallel()

	tq := orm.NewTestQuerier(orm.MySQL)
	q := newTestQuery(tq)

	_, _ = q.Scopes(
		scope.Where("name = ?", "alice"),
		scope.OrderBy("id DESC"),
		scope.Limit(5),
		scope.Offset(10),
	).All(t.Context())

	got := tq.LastQuery()
	want := "SELECT `id`, `name` FROM `users` WHERE name = ? ORDER BY id DESC LIMIT 5 OFFSET 10"
	if got.SQL != want {
		t.Errorf("SQL = %q, want %q", got.SQL, want)
	}
}

// --- Immutability ---

func TestQueryImmutability(t *testing.T) {
	t.Parallel()

	tq := orm.NewTestQuerier(orm.MySQL)
	base := newTestQuery(tq)

	_ = base.Where("name = ?", "alice")
	_ = base.OrderBy("id")
	_ = base.Limit(10)
	_ = base.Offset(5)

	_, _ = base.All(t.Context())

	got := tq.LastQuery()
	want := "SELECT `id`, `name` FROM `users`"
	if got.SQL != want {
		t.Errorf("base query was mutated: SQL = %q", got.SQL)
	}
}

// --- INSERT ---

func TestBuildInsertMySQL(t *testing.T) {
	t.Parallel()

	tq := orm.NewTestQuerier(orm.MySQL)
	q := newTestQuery(tq)

	u := testUser{Name: "alice"}
	_ = q.Create(t.Context(), &u)

	got := tq.LastQuery()
	want := "INSERT INTO `users` (`name`) VALUES (?)"
	if got.SQL != want {
		t.Errorf("SQL = %q, want %q", got.SQL, want)
	}
	if len(got.Args) != 1 || got.Args[0] != "alice" {
		t.Errorf("Args = %v", got.Args)
	}
}

func TestBuildInsertPostgreSQL(t *testing.T) {
	t.Parallel()

	tq := orm.NewTestQuerier(orm.PostgreSQL)
	q := newTestQuery(tq)

	u := testUser{Name: "alice"}
	_ = q.Create(t.Context(), &u)

	got := tq.LastQuery()
	want := `INSERT INTO "users" ("name") VALUES ($1) RETURNING "id"`
	if got.SQL != want {
		t.Errorf("SQL = %q, want %q", got.SQL, want)
	}
}

// --- UPDATE ---

func TestBuildUpdate(t *testing.T) {
	t.Parallel()

	tq := orm.NewTestQuerier(orm.MySQL)
	q := newTestQuery(tq)

	u := testUser{ID: 1, Name: "bob"}
	_ = q.Update(t.Context(), &u)

	got := tq.LastQuery()
	want := "UPDATE `users` SET `name` = ? WHERE `id` = ?"
	if got.SQL != want {
		t.Errorf("SQL = %q, want %q", got.SQL, want)
	}
	if len(got.Args) != 2 || got.Args[0] != "bob" || got.Args[1] != 1 {
		t.Errorf("Args = %v", got.Args)
	}
}

func TestBuildUpdatePostgreSQL(t *testing.T) {
	t.Parallel()

	tq := orm.NewTestQuerier(orm.PostgreSQL)
	q := newTestQuery(tq)

	u := testUser{ID: 1, Name: "bob"}
	_ = q.Update(t.Context(), &u)

	got := tq.LastQuery()
	want := `UPDATE "users" SET "name" = $1 WHERE "id" = $2`
	if got.SQL != want {
		t.Errorf("SQL = %q, want %q", got.SQL, want)
	}
}

// --- DELETE ---

func TestBuildDelete(t *testing.T) {
	t.Parallel()

	tq := orm.NewTestQuerier(orm.MySQL)
	q := newTestQuery(tq)

	_ = q.Where("id = ?", 1).Delete(t.Context())

	got := tq.LastQuery()
	want := "DELETE FROM `users` WHERE id = ?"
	if got.SQL != want {
		t.Errorf("SQL = %q, want %q", got.SQL, want)
	}
}

func TestDeleteWithoutWhereReturnsError(t *testing.T) {
	t.Parallel()

	tq := orm.NewTestQuerier(orm.MySQL)
	q := newTestQuery(tq)

	err := q.Delete(t.Context())
	if err == nil {
		t.Fatal("expected error for Delete without WHERE, got nil")
	}
}

// --- Rewrite (PostgreSQL placeholders) ---

func TestRewritePostgreSQLSelect(t *testing.T) {
	t.Parallel()

	tq := orm.NewTestQuerier(orm.PostgreSQL)
	q := newTestQuery(tq)

	_, _ = q.Where("name = ?", "alice").Where("id > ?", 10).All(t.Context())

	got := tq.LastQuery()
	want := `SELECT "id", "name" FROM "users" WHERE name = $1 AND id > $2`
	if got.SQL != want {
		t.Errorf("SQL = %q, want %q", got.SQL, want)
	}
}

// --- First ---

func TestFirstAddsLimit(t *testing.T) {
	t.Parallel()

	tq := orm.NewTestQuerier(orm.MySQL)
	q := newTestQuery(tq)

	_, _ = q.First(t.Context())

	got := tq.LastQuery()
	want := "SELECT `id`, `name` FROM `users` LIMIT 1"
	if got.SQL != want {
		t.Errorf("SQL = %q, want %q", got.SQL, want)
	}
}

// --- Timestamp tests ---

type testArticle struct {
	ID        int
	Title     string
	CreatedAt time.Time
	UpdatedAt time.Time
}

var testArticleColumns = []string{"id", "title", "created_at", "updated_at"}

func scanTestArticle(_ *sql.Rows) (testArticle, error) {
	return testArticle{}, nil
}

func testArticleColValPairs(a *testArticle, includesPK bool) ([]string, []any) {
	if includesPK {
		return []string{"id", "title", "created_at", "updated_at"},
			[]any{a.ID, a.Title, a.CreatedAt, a.UpdatedAt}
	}
	return []string{"title", "created_at", "updated_at"},
		[]any{a.Title, a.CreatedAt, a.UpdatedAt}
}

func setTestArticlePK(a *testArticle, id int64) {
	a.ID = int(id)
}

func setTestArticleCreatedAt(a *testArticle, now time.Time) {
	if a.CreatedAt.IsZero() {
		a.CreatedAt = now
	}
}

func setTestArticleUpdatedAt(a *testArticle, now time.Time) {
	a.UpdatedAt = now
}

func newTestArticleQuery(tq *orm.TestQuerier) *orm.Query[testArticle] {
	q := orm.NewQuery[testArticle](
		tq, "articles", testArticleColumns, "id", scanTestArticle, testArticleColValPairs, setTestArticlePK)
	q.RegisterTimestamps([]string{"created_at"}, setTestArticleCreatedAt, []string{"updated_at"}, setTestArticleUpdatedAt)
	return q
}

type fixedClock struct {
	t time.Time
}

func (c fixedClock) Now() time.Time { return c.t }

func TestUpsertExcludesCreatedAtFromUpdate(t *testing.T) {
	t.Parallel()

	tq := orm.NewTestQuerier(orm.MySQL)
	q := newTestArticleQuery(tq)

	a := testArticle{ID: 1, Title: "hello"}
	_ = q.Upsert(t.Context(), &a)

	got := tq.LastQuery()
	// UPDATE clause should NOT contain created_at
	if _, updatePart, ok := strings.Cut(got.SQL, "ON DUPLICATE KEY UPDATE"); ok {
		if strings.Contains(updatePart, "created_at") {
			t.Errorf("UPDATE clause should not contain created_at: %s", got.SQL)
		}
		if !strings.Contains(updatePart, "updated_at") {
			t.Errorf("UPDATE clause should contain updated_at: %s", got.SQL)
		}
	} else {
		t.Errorf("expected ON DUPLICATE KEY UPDATE in SQL: %s", got.SQL)
	}
}

func TestUpsertExcludesCreatedAtPostgreSQL(t *testing.T) {
	t.Parallel()

	tq := orm.NewTestQuerier(orm.PostgreSQL)
	q := newTestArticleQuery(tq)

	a := testArticle{ID: 1, Title: "hello"}
	_ = q.Upsert(t.Context(), &a)

	got := tq.LastQuery()
	// DO UPDATE SET should NOT contain created_at
	if _, updatePart, ok := strings.Cut(got.SQL, "DO UPDATE SET"); ok {
		if strings.Contains(updatePart, "created_at") {
			t.Errorf("UPDATE SET should not contain created_at: %s", got.SQL)
		}
		if !strings.Contains(updatePart, "updated_at") {
			t.Errorf("UPDATE SET should contain updated_at: %s", got.SQL)
		}
	} else {
		t.Errorf("expected DO UPDATE SET in SQL: %s", got.SQL)
	}
}

func TestCreateAutoSetsTimestamps(t *testing.T) {
	t.Parallel()

	fixed := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	ctx := orm.WithClock(t.Context(), fixedClock{t: fixed})

	tq := orm.NewTestQuerier(orm.MySQL)
	q := newTestArticleQuery(tq)

	a := testArticle{Title: "hello"}
	_ = q.Create(ctx, &a)

	if a.CreatedAt != fixed {
		t.Errorf("CreatedAt = %v, want %v", a.CreatedAt, fixed)
	}
	if a.UpdatedAt != fixed {
		t.Errorf("UpdatedAt = %v, want %v", a.UpdatedAt, fixed)
	}
}

func TestCreatePreservesExistingCreatedAt(t *testing.T) {
	t.Parallel()

	existing := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	fixed := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	ctx := orm.WithClock(t.Context(), fixedClock{t: fixed})

	tq := orm.NewTestQuerier(orm.MySQL)
	q := newTestArticleQuery(tq)

	a := testArticle{Title: "hello", CreatedAt: existing}
	_ = q.Create(ctx, &a)

	if a.CreatedAt != existing {
		t.Errorf("CreatedAt = %v, want %v (should not be overwritten)", a.CreatedAt, existing)
	}
	if a.UpdatedAt != fixed {
		t.Errorf("UpdatedAt = %v, want %v", a.UpdatedAt, fixed)
	}
}

func TestUpdateOnlySetsUpdatedAt(t *testing.T) {
	t.Parallel()

	existing := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	fixed := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	ctx := orm.WithClock(t.Context(), fixedClock{t: fixed})

	tq := orm.NewTestQuerier(orm.MySQL)
	q := newTestArticleQuery(tq)

	a := testArticle{ID: 1, Title: "hello", CreatedAt: existing}
	_ = q.Update(ctx, &a)

	if a.CreatedAt != existing {
		t.Errorf("CreatedAt = %v, want %v (Update should not touch createdAt)", a.CreatedAt, existing)
	}
	if a.UpdatedAt != fixed {
		t.Errorf("UpdatedAt = %v, want %v", a.UpdatedAt, fixed)
	}
}

// --- scope.Join / scope.LeftJoin / scope.Preload via Scopes ---

func TestBuildSelectWithScopeJoin(t *testing.T) {
	t.Parallel()

	tq := orm.NewTestQuerier(orm.MySQL)
	q := newTestQuery(tq)
	q.RegisterJoin("Posts", orm.JoinConfig{
		TargetTable:  "posts",
		TargetColumn: "user_id",
		SourceTable:  "users",
		SourceColumn: "id",
	})

	_, _ = q.Scopes(scope.Join("Posts")).All(t.Context())

	got := tq.LastQuery()
	if !strings.Contains(got.SQL, "INNER JOIN") {
		t.Errorf("SQL should contain INNER JOIN: %q", got.SQL)
	}
	want := "SELECT `users`.`id`, `users`.`name` FROM `users` INNER JOIN `posts` ON `posts`.`user_id` = `users`.`id`"
	if got.SQL != want {
		t.Errorf("SQL = %q, want %q", got.SQL, want)
	}
}

func TestBuildSelectWithScopeLeftJoin(t *testing.T) {
	t.Parallel()

	tq := orm.NewTestQuerier(orm.MySQL)
	q := newTestQuery(tq)
	q.RegisterJoin("Posts", orm.JoinConfig{
		TargetTable:  "posts",
		TargetColumn: "user_id",
		SourceTable:  "users",
		SourceColumn: "id",
	})

	_, _ = q.Scopes(scope.LeftJoin("Posts")).All(t.Context())

	got := tq.LastQuery()
	if !strings.Contains(got.SQL, "LEFT JOIN") {
		t.Errorf("SQL should contain LEFT JOIN: %q", got.SQL)
	}
	want := "SELECT `users`.`id`, `users`.`name` FROM `users` LEFT JOIN `posts` ON `posts`.`user_id` = `users`.`id`"
	if got.SQL != want {
		t.Errorf("SQL = %q, want %q", got.SQL, want)
	}
}

func TestBuildSelectWithJoinSelectColumns(t *testing.T) {
	t.Parallel()

	tq := orm.NewTestQuerier(orm.MySQL)
	q := newTestQuery(tq)
	q.RegisterJoin("Author", orm.JoinConfig{
		TargetTable:   "authors",
		TargetColumn:  "id",
		SourceTable:   "users",
		SourceColumn:  "author_id",
		SelectColumns: []string{"id", "name"},
	})

	_, _ = q.Join("Author").All(t.Context())

	got := tq.LastQuery()
	want := "SELECT `users`.`id`, `users`.`name`, `authors`.`id` AS `Author__id`, `authors`.`name` AS `Author__name`" +
		" FROM `users` INNER JOIN `authors` ON `authors`.`id` = `users`.`author_id`"
	if got.SQL != want {
		t.Errorf("SQL = %q, want %q", got.SQL, want)
	}
}

func TestBuildSelectWithJoinSelectColumnsPostgreSQL(t *testing.T) {
	t.Parallel()

	tq := orm.NewTestQuerier(orm.PostgreSQL)
	q := newTestQuery(tq)
	q.RegisterJoin("Author", orm.JoinConfig{
		TargetTable:   "authors",
		TargetColumn:  "id",
		SourceTable:   "users",
		SourceColumn:  "author_id",
		SelectColumns: []string{"id", "name"},
	})

	_, _ = q.Join("Author").All(t.Context())

	got := tq.LastQuery()
	want := `SELECT "users"."id", "users"."name", "authors"."id" AS "Author__id", "authors"."name" AS "Author__name"` +
		` FROM "users" INNER JOIN "authors" ON "authors"."id" = "users"."author_id"`
	if got.SQL != want {
		t.Errorf("SQL = %q, want %q", got.SQL, want)
	}
}

func TestBuildSelectWithJoinNoSelectColumns(t *testing.T) {
	t.Parallel()

	// When SelectColumns is nil (e.g. has_many), no extra columns are added.
	tq := orm.NewTestQuerier(orm.MySQL)
	q := newTestQuery(tq)
	q.RegisterJoin("Posts", orm.JoinConfig{
		TargetTable:  "posts",
		TargetColumn: "user_id",
		SourceTable:  "users",
		SourceColumn: "id",
	})

	_, _ = q.Join("Posts").All(t.Context())

	got := tq.LastQuery()
	want := "SELECT `users`.`id`, `users`.`name` FROM `users` INNER JOIN `posts` ON `posts`.`user_id` = `users`.`id`"
	if got.SQL != want {
		t.Errorf("SQL = %q, want %q", got.SQL, want)
	}
}

func TestBuildSelectWithScopePreload(t *testing.T) {
	t.Parallel()

	tq := orm.NewTestQuerier(orm.MySQL)
	q := newTestQuery(tq)
	q.RegisterPreloader("Posts", func(_ context.Context, _ orm.Querier, _ []testUser) error {
		return nil
	})

	// Scopes(scope.Preload("Posts")) should not affect the generated SQL;
	// preloads are executed after the main query.
	_, _ = q.Scopes(scope.Preload("Posts")).All(t.Context())

	got := tq.LastQuery()
	want := "SELECT `id`, `name` FROM `users`"
	if got.SQL != want {
		t.Errorf("SQL = %q, want %q (preload should not alter SQL)", got.SQL, want)
	}
}

// --- Updates ---

func TestUpdates(t *testing.T) {
	t.Parallel()

	tq := orm.NewTestQuerier(orm.MySQL)
	q := newTestQuery(tq)

	err := q.Where("id = ?", 123).Updates(t.Context(), map[string]any{
		"name": "new name",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := tq.LastQuery()
	want := "UPDATE `users` SET `name` = ? WHERE id = ?"
	if got.SQL != want {
		t.Errorf("SQL = %q, want %q", got.SQL, want)
	}
	if len(got.Args) != 2 || got.Args[0] != "new name" || got.Args[1] != 123 {
		t.Errorf("Args = %v", got.Args)
	}
}

func TestUpdatesPostgreSQL(t *testing.T) {
	t.Parallel()

	tq := orm.NewTestQuerier(orm.PostgreSQL)
	q := newTestQuery(tq)

	err := q.Where("id = ?", 123).Updates(t.Context(), map[string]any{
		"name": "new name",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := tq.LastQuery()
	want := `UPDATE "users" SET "name" = $1 WHERE id = $2`
	if got.SQL != want {
		t.Errorf("SQL = %q, want %q", got.SQL, want)
	}
}

func TestUpdatesAutoUpdatedAt(t *testing.T) {
	t.Parallel()

	fixed := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	ctx := orm.WithClock(t.Context(), fixedClock{t: fixed})

	tq := orm.NewTestQuerier(orm.MySQL)
	q := newTestArticleQuery(tq)

	err := q.Where("id = ?", 1).Updates(ctx, map[string]any{
		"title": "updated title",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := tq.LastQuery()
	// Should contain both title and updated_at in SET clause
	if !strings.Contains(got.SQL, "`updated_at` = ?") {
		t.Errorf("SQL should contain updated_at: %q", got.SQL)
	}
	if !strings.Contains(got.SQL, "`title` = ?") {
		t.Errorf("SQL should contain title: %q", got.SQL)
	}

	// Verify updated_at value is the fixed time
	foundUpdatedAt := false
	for _, arg := range got.Args {
		if ts, ok := arg.(time.Time); ok && ts.Equal(fixed) {
			foundUpdatedAt = true
			break
		}
	}
	if !foundUpdatedAt {
		t.Errorf("Args should contain fixed time %v: %v", fixed, got.Args)
	}
}

// --- FOR UPDATE ---

func TestBuildSelectForUpdate(t *testing.T) {
	t.Parallel()

	tq := orm.NewTestQuerier(orm.MySQL)
	q := newTestQuery(tq)

	_, _ = q.Where("id = ?", 1).ForUpdate().All(t.Context())

	got := tq.LastQuery()
	want := "SELECT `id`, `name` FROM `users` WHERE id = ? FOR UPDATE"
	if got.SQL != want {
		t.Errorf("SQL = %q, want %q", got.SQL, want)
	}
}

func TestBuildSelectForUpdatePostgreSQL(t *testing.T) {
	t.Parallel()

	tq := orm.NewTestQuerier(orm.PostgreSQL)
	q := newTestQuery(tq)

	_, _ = q.Where("id = ?", 1).ForUpdate().All(t.Context())

	got := tq.LastQuery()
	want := `SELECT "id", "name" FROM "users" WHERE id = $1 FOR UPDATE`
	if got.SQL != want {
		t.Errorf("SQL = %q, want %q", got.SQL, want)
	}
}

func TestBuildSelectForUpdateWithScope(t *testing.T) {
	t.Parallel()

	tq := orm.NewTestQuerier(orm.MySQL)
	q := newTestQuery(tq)

	_, _ = q.Scopes(scope.Where("id = ?", 1), scope.ForUpdate()).All(t.Context())

	got := tq.LastQuery()
	want := "SELECT `id`, `name` FROM `users` WHERE id = ? FOR UPDATE"
	if got.SQL != want {
		t.Errorf("SQL = %q, want %q", got.SQL, want)
	}
}

func TestForUpdateImmutability(t *testing.T) {
	t.Parallel()

	tq := orm.NewTestQuerier(orm.MySQL)
	base := newTestQuery(tq)

	_ = base.ForUpdate()

	_, _ = base.All(t.Context())

	got := tq.LastQuery()
	want := "SELECT `id`, `name` FROM `users`"
	if got.SQL != want {
		t.Errorf("base query was mutated: SQL = %q, want %q", got.SQL, want)
	}
}

func TestUpdatesWithoutWhereReturnsError(t *testing.T) {
	t.Parallel()

	tq := orm.NewTestQuerier(orm.MySQL)
	q := newTestQuery(tq)

	err := q.Updates(t.Context(), map[string]any{"name": "oops"})
	if err == nil {
		t.Fatal("expected error for Updates without WHERE, got nil")
	}
}

// --- Fix regressions: Update ---

func TestUpdateExcludesCreatedAt(t *testing.T) {
	t.Parallel()

	tq := orm.NewTestQuerier(orm.MySQL)
	q := newTestArticleQuery(tq)

	a := testArticle{ID: 1, Title: "hello", CreatedAt: time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)}
	if err := q.Update(t.Context(), &a); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got := tq.LastQuery()
	want := "UPDATE `articles` SET `title` = ?, `updated_at` = ? WHERE `id` = ?"
	if got.SQL != want {
		t.Errorf("SQL = %q, want %q", got.SQL, want)
	}
}

func TestUpdateRequiresPrimaryKey(t *testing.T) {
	t.Parallel()

	tq := orm.NewTestQuerier(orm.MySQL)
	q := newTestQuery(tq)

	u := testUser{Name: "no id"}
	err := q.Update(t.Context(), &u)
	if err == nil {
		t.Fatal("expected error for Update with zero primary key, got nil")
	}
	if len(tq.Queries) != 0 {
		t.Errorf("no query should run, got %v", tq.Queries)
	}
}

// --- Fix regressions: Count / Exists ---

func TestCountIgnoresLimitOffset(t *testing.T) {
	t.Parallel()

	tq := orm.NewTestQuerier(orm.MySQL)
	tq.Results = []orm.TestRows{{Cols: []string{"count"}, Rows: [][]driver.Value{{int64(42)}}}}
	q := newTestQuery(tq)

	count, err := q.Limit(10).Offset(20).Count(t.Context())
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count != 42 {
		t.Errorf("count = %d, want 42", count)
	}

	got := tq.LastQuery()
	want := "SELECT COUNT(*) FROM `users`"
	if got.SQL != want {
		t.Errorf("SQL = %q, want %q", got.SQL, want)
	}
}

func TestCountWithJoinCountsDistinctParents(t *testing.T) {
	t.Parallel()

	tq := orm.NewTestQuerier(orm.MySQL)
	tq.Results = []orm.TestRows{{Cols: []string{"count"}, Rows: [][]driver.Value{{int64(5)}}}}
	q := newTestQuery(tq)
	q.RegisterJoin("Posts", orm.JoinConfig{
		TargetTable:  "posts",
		TargetColumn: "user_id",
		SourceTable:  "users",
		SourceColumn: "id",
	})

	if _, err := q.Join("Posts").Count(t.Context()); err != nil {
		t.Fatalf("Count: %v", err)
	}

	got := tq.LastQuery()
	want := "SELECT COUNT(DISTINCT `users`.`id`) FROM `users` INNER JOIN `posts` ON `posts`.`user_id` = `users`.`id`"
	if got.SQL != want {
		t.Errorf("SQL = %q, want %q", got.SQL, want)
	}
}

func TestExistsSelectsOneRow(t *testing.T) {
	t.Parallel()

	tq := orm.NewTestQuerier(orm.MySQL)
	tq.Results = []orm.TestRows{{Cols: []string{"1"}, Rows: [][]driver.Value{{int64(1)}}}}
	q := newTestQuery(tq)

	exists, err := q.Where("id = ?", 1).Exists(t.Context())
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if !exists {
		t.Error("exists = false, want true")
	}

	got := tq.LastQuery()
	want := "SELECT 1 FROM `users` WHERE id = ? LIMIT 1"
	if got.SQL != want {
		t.Errorf("SQL = %q, want %q", got.SQL, want)
	}
}

func TestExistsFalseOnNoRows(t *testing.T) {
	t.Parallel()

	tq := orm.NewTestQuerier(orm.MySQL)
	tq.Results = []orm.TestRows{{Cols: []string{"1"}, Rows: nil}}
	q := newTestQuery(tq)

	exists, err := q.Where("id = ?", 1).Exists(t.Context())
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if exists {
		t.Error("exists = true, want false")
	}
}

// --- Fix regressions: OFFSET without LIMIT ---

func TestBuildSelectOffsetWithoutLimitMySQL(t *testing.T) {
	t.Parallel()

	tq := orm.NewTestQuerier(orm.MySQL)
	q := newTestQuery(tq)

	_, _ = q.Offset(10).All(t.Context())

	got := tq.LastQuery()
	want := "SELECT `id`, `name` FROM `users` LIMIT 18446744073709551615 OFFSET 10"
	if got.SQL != want {
		t.Errorf("SQL = %q, want %q", got.SQL, want)
	}
}

func TestBuildSelectOffsetWithoutLimitPostgreSQL(t *testing.T) {
	t.Parallel()

	tq := orm.NewTestQuerier(orm.PostgreSQL)
	q := newTestQuery(tq)

	_, _ = q.Offset(10).All(t.Context())

	got := tq.LastQuery()
	want := `SELECT "id", "name" FROM "users" OFFSET 10`
	if got.SQL != want {
		t.Errorf("SQL = %q, want %q", got.SQL, want)
	}
}

// --- Fix regressions: placeholder rewrite ---

func TestRewriteKeepsPlaceholdersInsideStringLiterals(t *testing.T) {
	t.Parallel()

	tq := orm.NewTestQuerier(orm.PostgreSQL)
	q := newTestQuery(tq)

	_, _ = q.Where("name = 'wh?o' AND note = 'it''s ?' AND id = ?", 1).All(t.Context())

	got := tq.LastQuery()
	want := `SELECT "id", "name" FROM "users" WHERE name = 'wh?o' AND note = 'it''s ?' AND id = $1`
	if got.SQL != want {
		t.Errorf("SQL = %q, want %q", got.SQL, want)
	}
}

// --- Fix regressions: Updates ---

func TestUpdatesDoesNotMutateCallerMap(t *testing.T) {
	t.Parallel()

	tq := orm.NewTestQuerier(orm.MySQL)
	q := newTestArticleQuery(tq)

	values := map[string]any{"title": "new"}
	if err := q.Where("id = ?", 1).Updates(t.Context(), values); err != nil {
		t.Fatalf("Updates: %v", err)
	}

	if len(values) != 1 {
		t.Errorf("caller map mutated: %v", values)
	}
}

func TestUpdatesNilMapReturnsError(t *testing.T) {
	t.Parallel()

	tq := orm.NewTestQuerier(orm.MySQL)
	q := newTestQuery(tq)

	err := q.Where("id = ?", 1).Updates(t.Context(), nil)
	if err == nil {
		t.Fatal("expected error for Updates with no columns, got nil")
	}
}

func TestUpdatesSortsColumns(t *testing.T) {
	t.Parallel()

	tq := orm.NewTestQuerier(orm.MySQL)
	q := newTestQuery(tq)

	err := q.Where("id = ?", 1).Updates(t.Context(), map[string]any{
		"name":  "x",
		"email": "y",
	})
	if err != nil {
		t.Fatalf("Updates: %v", err)
	}

	got := tq.LastQuery()
	want := "UPDATE `users` SET `email` = ?, `name` = ? WHERE id = ?"
	if got.SQL != want {
		t.Errorf("SQL = %q, want %q", got.SQL, want)
	}
}

// --- Fix regressions: joins ---

func TestJoinUnknownNameErrors(t *testing.T) {
	t.Parallel()

	tq := orm.NewTestQuerier(orm.MySQL)
	q := newTestQuery(tq)

	_, err := q.Join("Nope").All(t.Context())
	if err == nil || !strings.Contains(err.Error(), "unknown join") {
		t.Fatalf("err = %v, want unknown join error", err)
	}
	if len(tq.Queries) != 0 {
		t.Errorf("no query should run, got %v", tq.Queries)
	}
}

func TestJoinTwiceAddsOneClause(t *testing.T) {
	t.Parallel()

	tq := orm.NewTestQuerier(orm.MySQL)
	q := newTestQuery(tq)
	q.RegisterJoin("Posts", orm.JoinConfig{
		TargetTable:  "posts",
		TargetColumn: "user_id",
		SourceTable:  "users",
		SourceColumn: "id",
	})

	_, _ = q.Join("Posts").Join("Posts").All(t.Context())

	got := tq.LastQuery()
	if n := strings.Count(got.SQL, "INNER JOIN"); n != 1 {
		t.Errorf("INNER JOIN appears %d times, want 1: %q", n, got.SQL)
	}
}

func TestRegisterJoinOnDerivedDoesNotAffectParent(t *testing.T) {
	t.Parallel()

	tq := orm.NewTestQuerier(orm.MySQL)
	base := newTestQuery(tq)
	derived := base.Where("id = ?", 1)
	derived.RegisterJoin("Posts", orm.JoinConfig{
		TargetTable:  "posts",
		TargetColumn: "user_id",
		SourceTable:  "users",
		SourceColumn: "id",
	})

	_, _ = derived.Join("Posts").All(t.Context())
	if !strings.Contains(tq.LastQuery().SQL, "INNER JOIN") {
		t.Fatalf("derived join missing: %q", tq.LastQuery().SQL)
	}

	_, err := base.Join("Posts").All(t.Context())
	if err == nil || !strings.Contains(err.Error(), "unknown join") {
		t.Errorf("register leaked into parent: err = %v", err)
	}
}

// --- Read paths through the fake driver ---

func TestAllScansRowsAndRunsPreloaders(t *testing.T) {
	t.Parallel()

	tq := orm.NewTestQuerier(orm.MySQL)
	tq.Results = []orm.TestRows{{
		Cols: []string{"id", "name"},
		Rows: [][]driver.Value{{int64(1), "alice"}, {int64(2), "bob"}},
	}}

	q := newTestScanQuery(tq)
	var preloaded int
	q.RegisterPreloader("Posts", func(_ context.Context, _ orm.Querier, users []testUser) error {
		preloaded = len(users)
		return nil
	})

	users, err := q.Preload("Posts").All(t.Context())
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(users) != 2 || users[0].Name != "alice" || users[1].ID != 2 {
		t.Errorf("users = %+v", users)
	}
	if preloaded != 2 {
		t.Errorf("preloader saw %d rows, want 2", preloaded)
	}
}

func TestFirstReturnsErrNotFound(t *testing.T) {
	t.Parallel()

	tq := orm.NewTestQuerier(orm.MySQL)
	tq.Results = []orm.TestRows{{Cols: []string{"id", "name"}, Rows: nil}}

	q := newTestScanQuery(tq)
	_, err := q.First(t.Context())
	if !errors.Is(err, orm.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestCreateMySQLSetsPKFromLastInsertId(t *testing.T) {
	t.Parallel()

	tq := orm.NewTestQuerier(orm.MySQL)
	tq.LastID = 9
	q := newTestQuery(tq)

	u := testUser{Name: "alice"}
	if err := q.Create(t.Context(), &u); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if u.ID != 9 {
		t.Errorf("ID = %d, want 9", u.ID)
	}
}

func TestCreatePostgreSQLSetsPKFromReturning(t *testing.T) {
	t.Parallel()

	tq := orm.NewTestQuerier(orm.PostgreSQL)
	tq.Results = []orm.TestRows{{Cols: []string{"id"}, Rows: [][]driver.Value{{int64(7)}}}}
	q := newTestQuery(tq)

	u := testUser{Name: "alice"}
	if err := q.Create(t.Context(), &u); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if u.ID != 7 {
		t.Errorf("ID = %d, want 7", u.ID)
	}
}

func TestCreateAllPostgreSQLSetsPKsFromReturning(t *testing.T) {
	t.Parallel()

	tq := orm.NewTestQuerier(orm.PostgreSQL)
	tq.Results = []orm.TestRows{{Cols: []string{"id"}, Rows: [][]driver.Value{{int64(1)}, {int64(2)}}}}
	q := newTestQuery(tq)

	items := []*testUser{{Name: "a"}, {Name: "b"}}
	if err := q.CreateAll(t.Context(), items); err != nil {
		t.Fatalf("CreateAll: %v", err)
	}
	if items[0].ID != 1 || items[1].ID != 2 {
		t.Errorf("IDs = %d, %d, want 1, 2", items[0].ID, items[1].ID)
	}
}

func TestCreateAllReturningRowCountMismatch(t *testing.T) {
	t.Parallel()

	tq := orm.NewTestQuerier(orm.PostgreSQL)
	tq.Results = []orm.TestRows{{
		Cols: []string{"id"},
		Rows: [][]driver.Value{{int64(1)}, {int64(2)}, {int64(3)}},
	}}
	q := newTestQuery(tq)

	items := []*testUser{{Name: "a"}, {Name: "b"}}
	err := q.CreateAll(t.Context(), items)
	if err == nil || !strings.Contains(err.Error(), "more rows than items") {
		t.Fatalf("err = %v, want row count mismatch error", err)
	}
}

// --- Transaction on Querier ---

func TestQuerierTransactionRunsAgainstMock(t *testing.T) {
	t.Parallel()

	tq := orm.NewTestQuerier(orm.MySQL)

	err := tq.Transaction(t.Context(), func(q orm.Querier) error {
		u := testUser{Name: "alice"}
		return orm.NewQuery[testUser](
			q, "users", testUserColumns, "id", scanTestUser, testUserColValPairs, setTestUserPK,
		).Create(t.Context(), &u)
	})
	if err != nil {
		t.Fatalf("Transaction: %v", err)
	}

	got := tq.LastQuery()
	want := "INSERT INTO `users` (`name`) VALUES (?)"
	if got.SQL != want {
		t.Errorf("SQL = %q, want %q", got.SQL, want)
	}
}

// --- PK + created_at only: nothing to SET / nothing to update on conflict ---

type testMarker struct {
	ID        int
	CreatedAt time.Time
}

func newTestMarkerQuery(tq *orm.TestQuerier) *orm.Query[testMarker] {
	q := orm.NewQuery[testMarker](
		tq, "markers", []string{"id", "created_at"}, "id",
		func(*sql.Rows) (testMarker, error) { return testMarker{}, nil },
		func(m *testMarker, includesPK bool) ([]string, []any) {
			if includesPK {
				return []string{"id", "created_at"}, []any{m.ID, m.CreatedAt}
			}
			return []string{"created_at"}, []any{m.CreatedAt}
		},
		nil,
	)
	q.RegisterTimestamps([]string{"created_at"}, func(m *testMarker, now time.Time) {
		if m.CreatedAt.IsZero() {
			m.CreatedAt = now
		}
	}, nil, nil)
	return q
}

func TestUpdateWithNoSettableColumnsErrors(t *testing.T) {
	t.Parallel()

	tq := orm.NewTestQuerier(orm.MySQL)
	q := newTestMarkerQuery(tq)

	m := testMarker{ID: 1}
	err := q.Update(t.Context(), &m)
	if err == nil {
		t.Fatal("expected error for Update with no settable columns, got nil")
	}
	if len(tq.Queries) != 0 {
		t.Errorf("no query should run, got %v", tq.Queries)
	}
}

func TestUpsertWithNoUpdatableColumnsMySQL(t *testing.T) {
	t.Parallel()

	tq := orm.NewTestQuerier(orm.MySQL)
	q := newTestMarkerQuery(tq)

	m := testMarker{ID: 1}
	if err := q.Upsert(t.Context(), &m); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	got := tq.LastQuery()
	want := "INSERT INTO `markers` (`id`, `created_at`) VALUES (?, ?) ON DUPLICATE KEY UPDATE `id` = `id`"
	if got.SQL != want {
		t.Errorf("SQL = %q, want %q", got.SQL, want)
	}
}

func TestUpsertWithNoUpdatableColumnsPostgreSQL(t *testing.T) {
	t.Parallel()

	tq := orm.NewTestQuerier(orm.PostgreSQL)
	q := newTestMarkerQuery(tq)

	m := testMarker{ID: 1}
	if err := q.Upsert(t.Context(), &m); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	got := tq.LastQuery()
	want := `INSERT INTO "markers" ("id", "created_at") VALUES ($1, $2) ON CONFLICT ("id") DO NOTHING`
	if got.SQL != want {
		t.Errorf("SQL = %q, want %q", got.SQL, want)
	}
}

func TestUpsertRequiresPrimaryKey(t *testing.T) {
	t.Parallel()

	tq := orm.NewTestQuerier(orm.MySQL)
	q := newTestQuery(tq)

	u := testUser{Name: "no id"}
	err := q.Upsert(t.Context(), &u)
	if err == nil {
		t.Fatal("expected error for Upsert with zero primary key, got nil")
	}
	if len(tq.Queries) != 0 {
		t.Errorf("no query should run, got %v", tq.Queries)
	}
}

func TestRewriteKeepsPlaceholdersInsideDollarQuotedStrings(t *testing.T) {
	t.Parallel()

	tq := orm.NewTestQuerier(orm.PostgreSQL)
	q := newTestQuery(tq)

	_, _ = q.Where("body = $$?$$ AND note = $tag$it's ?$tag$ AND price > 1$ AND id = ?", 1).All(t.Context())

	got := tq.LastQuery()
	want := `SELECT "id", "name" FROM "users" WHERE body = $$?$$ AND note = $tag$it's ?$tag$ AND price > 1$ AND id = $1`
	if got.SQL != want {
		t.Errorf("SQL = %q, want %q", got.SQL, want)
	}
}
