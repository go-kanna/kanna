package orm

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
)

var errMockNotImplemented = errors.New("mock: not implemented")

// TestRows is one result set served by TestQuerier.QueryContext. Values must
// be driver.Value kinds (int64, string, time.Time, …), the same constraint a
// real driver puts on scanned data.
type TestRows struct {
	Cols []string
	Rows [][]driver.Value
}

// TestQuerier is a mock Querier that records executed queries and serves
// queued result sets. Exported for use in the orm_test package.
type TestQuerier struct {
	D       Dialect
	Queries []TestQuery
	Results []TestRows // consumed by QueryContext in call order; empty → error
	LastID  int64      // what ExecContext's result reports for LastInsertId

	db *sql.DB
}

// TestQuery holds a captured query string and its args.
type TestQuery struct {
	SQL  string
	Args []any
}

// NewTestQuerier creates a TestQuerier with the given Dialect.
func NewTestQuerier(d Dialect) *TestQuerier {
	return &TestQuerier{D: d}
}

func (tq *TestQuerier) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	tq.Queries = append(tq.Queries, TestQuery{query, args})
	if len(tq.Results) == 0 {
		return nil, errMockNotImplemented
	}
	if tq.db == nil {
		tq.db = sql.OpenDB(testConnector{tq})
	}
	return tq.db.QueryContext(ctx, query, args...) //nolint:wrapcheck // thin wrapper
}

func (tq *TestQuerier) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	tq.Queries = append(tq.Queries, TestQuery{query, args})
	return testResult{lastID: tq.LastID}, nil
}

// Transaction joins, the same way Tx does: fn runs against the mock itself,
// which is what lets code written for Querier be unit-tested without a
// database.
func (tq *TestQuerier) Transaction(_ context.Context, fn func(q Querier) error) error {
	return fn(tq)
}

var _ Querier = (*TestQuerier)(nil)

// LastQuery returns the most recently captured query, or panics if empty.
func (tq *TestQuerier) LastQuery() TestQuery {
	return tq.Queries[len(tq.Queries)-1]
}

func (tq *TestQuerier) dialect() Dialect { return tq.D }

type testResult struct{ lastID int64 }

func (r testResult) LastInsertId() (int64, error) { return r.lastID, nil }
func (testResult) RowsAffected() (int64, error)   { return 0, nil }

// The fake driver below exists because *sql.Rows cannot be constructed by
// hand: the only way to hand real rows to the scan path is through
// database/sql itself.

type testConnector struct{ tq *TestQuerier }

func (c testConnector) Connect(context.Context) (driver.Conn, error) { return testConn(c), nil }
func (c testConnector) Driver() driver.Driver                        { return testDriver{} }

type testDriver struct{}

func (testDriver) Open(string) (driver.Conn, error) { return nil, errMockNotImplemented }

type testConn struct{ tq *TestQuerier }

func (testConn) Prepare(string) (driver.Stmt, error) { return nil, errMockNotImplemented }
func (testConn) Close() error                        { return nil }
func (testConn) Begin() (driver.Tx, error)           { return nil, errMockNotImplemented }

var _ driver.QueryerContext = testConn{}

func (c testConn) QueryContext(_ context.Context, _ string, _ []driver.NamedValue) (driver.Rows, error) {
	r := c.tq.Results[0]
	c.tq.Results = c.tq.Results[1:]
	return &testRows{cols: r.Cols, rows: r.Rows}, nil
}

type testRows struct {
	cols []string
	rows [][]driver.Value
	i    int
}

func (r *testRows) Columns() []string { return r.cols }
func (r *testRows) Close() error      { return nil }

func (r *testRows) Next(dest []driver.Value) error {
	if r.i >= len(r.rows) {
		return io.EOF
	}
	copy(dest, r.rows[r.i])
	r.i++
	return nil
}
