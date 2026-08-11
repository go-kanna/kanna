package orm_test

import (
	"testing"

	"github.com/go-kanna/kanna/orm"
)

func TestMySQLPlaceholder(t *testing.T) {
	t.Parallel()

	for _, index := range []int{1, 2, 10} {
		if got := orm.MySQL.Placeholder(index); got != "?" {
			t.Errorf("Placeholder(%d) = %q, want %q", index, got, "?")
		}
	}
}

func TestMySQLUseReturning(t *testing.T) {
	t.Parallel()

	if orm.MySQL.UseReturning() {
		t.Error("MySQL.UseReturning() = true, want false")
	}
}

func TestMySQLReturningClause(t *testing.T) {
	t.Parallel()

	if got := orm.MySQL.ReturningClause("id"); got != "" {
		t.Errorf("MySQL.ReturningClause(\"id\") = %q, want %q", got, "")
	}
}

func TestPostgreSQLPlaceholder(t *testing.T) {
	t.Parallel()

	tests := []struct {
		index int
		want  string
	}{
		{1, "$1"},
		{2, "$2"},
		{10, "$10"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()

			if got := orm.PostgreSQL.Placeholder(tt.index); got != tt.want {
				t.Errorf("Placeholder(%d) = %q, want %q", tt.index, got, tt.want)
			}
		})
	}
}

func TestPostgreSQLUseReturning(t *testing.T) {
	t.Parallel()

	if !orm.PostgreSQL.UseReturning() {
		t.Error("PostgreSQL.UseReturning() = false, want true")
	}
}

func TestPostgreSQLReturningClause(t *testing.T) {
	t.Parallel()

	want := ` RETURNING "id"`
	if got := orm.PostgreSQL.ReturningClause("id"); got != want {
		t.Errorf("PostgreSQL.ReturningClause(\"id\") = %q, want %q", got, want)
	}
}

func TestMySQLQuoteIdent(t *testing.T) {
	t.Parallel()

	if got := orm.MySQL.QuoteIdent("order"); got != "`order`" {
		t.Errorf("QuoteIdent = %q, want %q", got, "`order`")
	}
}

func TestPostgreSQLQuoteIdent(t *testing.T) {
	t.Parallel()

	want := `"order"`
	if got := orm.PostgreSQL.QuoteIdent("order"); got != want {
		t.Errorf("QuoteIdent = %q, want %q", got, want)
	}
}

func TestQuoteIdentEscapesEmbeddedQuotes(t *testing.T) {
	t.Parallel()

	if got, want := orm.MySQL.QuoteIdent("weird`name"), "`weird``name`"; got != want {
		t.Errorf("MySQL.QuoteIdent = %s, want %s", got, want)
	}
	if got, want := orm.PostgreSQL.QuoteIdent(`weird"name`), `"weird""name"`; got != want {
		t.Errorf("PostgreSQL.QuoteIdent = %s, want %s", got, want)
	}
}

func TestReturningClauseEscapesEmbeddedQuotes(t *testing.T) {
	t.Parallel()

	if got, want := orm.PostgreSQL.ReturningClause(`weird"pk`), ` RETURNING "weird""pk"`; got != want {
		t.Errorf("ReturningClause = %s, want %s", got, want)
	}
}
