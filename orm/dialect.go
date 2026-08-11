package orm

import (
	"fmt"
	"strings"
)

// Dialect abstracts SQL differences between database engines.
type Dialect interface {
	// Placeholder returns the bind parameter placeholder for the given
	// 1-based index. MySQL returns "?" regardless of index; PostgreSQL
	// returns "$1", "$2", etc.
	Placeholder(index int) string

	// QuoteIdent quotes an identifier (table name, column name) to safely
	// handle SQL reserved words. MySQL uses backticks; PostgreSQL uses
	// double quotes.
	QuoteIdent(name string) string

	// UseReturning reports whether INSERT should use a RETURNING clause
	// to retrieve the auto-generated primary key (PostgreSQL) rather
	// than relying on LastInsertId (MySQL).
	UseReturning() bool

	// ReturningClause returns the RETURNING clause appended to INSERT
	// statements. Returns an empty string for dialects that do not
	// support RETURNING (MySQL).
	ReturningClause(pk string) string
}

// MySQL is the Dialect for MySQL / MariaDB.
var MySQL Dialect = mysqlDialect{}

// PostgreSQL is the Dialect for PostgreSQL.
var PostgreSQL Dialect = postgresDialect{}

type mysqlDialect struct{}

func (mysqlDialect) Placeholder(_ int) string { return "?" }

// QuoteIdent doubles embedded backticks so a quote character inside an
// identifier cannot terminate the quoting.
func (mysqlDialect) QuoteIdent(name string) string {
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}
func (mysqlDialect) UseReturning() bool              { return false }
func (mysqlDialect) ReturningClause(_ string) string { return "" }

type postgresDialect struct{}

func (postgresDialect) Placeholder(index int) string { return fmt.Sprintf("$%d", index) }

// QuoteIdent doubles embedded double quotes so a quote character inside an
// identifier cannot terminate the quoting.
func (postgresDialect) QuoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}
func (postgresDialect) UseReturning() bool { return true }
func (d postgresDialect) ReturningClause(pk string) string {
	return " RETURNING " + d.QuoteIdent(pk)
}

// rewritePlaceholders converts ? placeholders to the dialect's form ($1, $2, …
// for PostgreSQL; MySQL keeps ? and returns early). Question marks inside
// single-quoted strings, quoted identifiers, dollar-quoted strings, and
// E'…' escape strings are left alone: a doubled closing quote stays inside
// its region, a backslash inside an escape string escapes the next byte, and
// an unterminated quote runs to the end. PostgreSQL's JSONB ?/?|/?&
// operators are indistinguishable from placeholders at this level — use the
// jsonb_exists* functions in clauses instead.
func rewritePlaceholders(d Dialect, query string) string {
	if _, ok := d.(mysqlDialect); ok {
		return query
	}
	var b strings.Builder
	b.Grow(len(query) + 8)
	idx := 1
	var quote byte
	var escString bool
	for i := 0; i < len(query); i++ {
		c := query[i]
		switch {
		case quote != 0:
			b.WriteByte(c)
			switch {
			case escString && c == '\\':
				// In an escape string a backslash escapes the next byte, so
				// \' or \\ never terminates the literal.
				if i+1 < len(query) {
					b.WriteByte(query[i+1])
					i++
				}
			case c == quote:
				if i+1 < len(query) && query[i+1] == quote {
					b.WriteByte(query[i+1])
					i++
				} else {
					quote = 0
					escString = false
				}
			}
		case c == '\'' || c == '"' || c == '`':
			quote = c
			// E'…' opens a PostgreSQL escape string — unless the E ends an
			// identifier or keyword (LIKE'…'), which opens a standard string.
			escString = c == '\'' && i >= 1 &&
				(query[i-1] == 'E' || query[i-1] == 'e') &&
				(i < 2 || !isIdentPart(query[i-2]))
			b.WriteByte(c)
		case c == '$':
			if end, ok := dollarQuote(query, i); ok {
				b.WriteString(query[i:end])
				i = end - 1
			} else {
				b.WriteByte(c)
			}
		case c == '?':
			b.WriteString(d.Placeholder(idx))
			idx++
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// dollarQuote reports the end (exclusive) of the PostgreSQL dollar-quoted
// string opening at start, where query[start] is '$'. An unterminated quote
// runs to the end of the query, the same way an unterminated single quote
// does. ok is false when start opens no dollar quote at all — a bare dollar
// sign, or a positional parameter like $1.
func dollarQuote(query string, start int) (int, bool) {
	j := start + 1
	if j < len(query) && isIdentStart(query[j]) {
		for j < len(query) && isIdentPart(query[j]) {
			j++
		}
	}
	if j >= len(query) || query[j] != '$' {
		return 0, false
	}
	delim := query[start : j+1]
	rest := strings.Index(query[j+1:], delim)
	if rest < 0 {
		return len(query), true
	}
	return j + 1 + rest + len(delim), true
}

func isIdentStart(c byte) bool {
	return c == '_' || ('a' <= c && c <= 'z') || ('A' <= c && c <= 'Z')
}

func isIdentPart(c byte) bool {
	return isIdentStart(c) || ('0' <= c && c <= '9')
}
