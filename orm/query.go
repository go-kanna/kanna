package orm

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/go-kanna/kanna/orm/scope"
)

// ScanFunc scans a single row into T.
// Generated per-type by kanna-orm.
type ScanFunc[T any] func(rows *sql.Rows) (T, error)

// ColumnValueFunc extracts column names and their values from a *T.
// When includesPK is false the primary key column is excluded (for INSERT
// with auto-increment).
type ColumnValueFunc[T any] func(t *T, includesPK bool) (columns []string, values []any)

// SetPKFunc sets the auto-generated primary key on *T after INSERT.
// May be nil when the primary key is not auto-generated.
type SetPKFunc[T any] func(t *T, id int64)

// SetCreatedAtFunc sets the createdAt timestamp on *T.
// The implementation should only set the field if its current value is zero.
// Generated per-type by kanna-orm; nil when no createdAt field exists.
type SetCreatedAtFunc[T any] func(t *T, now time.Time)

// SetUpdatedAtFunc sets the updatedAt timestamp on *T.
// Generated per-type by kanna-orm; nil when no updatedAt field exists.
type SetUpdatedAtFunc[T any] func(t *T, now time.Time)

// PreloaderFunc executes a preload query and assigns results to the parent slice.
// Generated per-relation by kanna-orm.
type PreloaderFunc[T any] func(ctx context.Context, db Querier, results []T) error

// JoinConfig holds the metadata needed to build a JOIN clause at runtime.
type JoinConfig struct {
	TargetTable   string
	TargetColumn  string
	SourceTable   string
	SourceColumn  string
	SelectColumns []string // target table columns to SELECT with aliases (nil = no extra SELECT)
}

// Query represents a pending query against a single table.
// All builder methods return a new Query; the receiver is never modified.
type Query[T any] struct {
	db          Querier
	table       string
	columns     []string
	pk          string
	scan        ScanFunc[T]
	colValPairs ColumnValueFunc[T]
	setPK       SetPKFunc[T]

	wheres              []whereClause
	orderBys            []string
	joins               []string
	selects             *string
	limit               *int
	offset              *int
	forUpdate           bool
	forUpdateSkipLocked bool

	joinDefs        map[string]JoinConfig
	activeJoinNames []string
	preloaders      map[string]PreloaderFunc[T]
	preloads        []string

	createdAtCols []string
	updatedAtCols []string
	setCreatedAt  SetCreatedAtFunc[T]
	setUpdatedAt  SetUpdatedAtFunc[T]

	// err defers a builder-stage mistake (an unknown join name) to the next
	// terminal method, since builder methods have no error return.
	err error
}

type whereClause struct {
	clause string
	args   []any
}

// NewQuery is called by generated factory functions.
func NewQuery[T any](
	db Querier,
	table string,
	columns []string,
	pk string,
	scan ScanFunc[T],
	colValPairs ColumnValueFunc[T],
	setPK SetPKFunc[T],
) *Query[T] {
	return &Query[T]{
		db:          db,
		table:       table,
		columns:     columns,
		pk:          pk,
		scan:        scan,
		colValPairs: colValPairs,
		setPK:       setPK,
	}
}

// RegisterJoin registers a named join definition for use with Join/LeftJoin.
func (q *Query[T]) RegisterJoin(name string, cfg JoinConfig) {
	if q.joinDefs == nil {
		q.joinDefs = make(map[string]JoinConfig)
	}
	q.joinDefs[name] = cfg
}

// RegisterPreloader registers a named preloader for use with Preload.
func (q *Query[T]) RegisterPreloader(name string, fn PreloaderFunc[T]) {
	if q.preloaders == nil {
		q.preloaders = make(map[string]PreloaderFunc[T])
	}
	q.preloaders[name] = fn
}

// RegisterTimestamps configures automatic timestamp management.
func (q *Query[T]) RegisterTimestamps(
	createdAtCols []string, setCreatedAt SetCreatedAtFunc[T],
	updatedAtCols []string, setUpdatedAt SetUpdatedAtFunc[T],
) {
	q.createdAtCols = createdAtCols
	q.updatedAtCols = updatedAtCols
	q.setCreatedAt = setCreatedAt
	q.setUpdatedAt = setUpdatedAt
}

// clone returns a shallow copy with slices copied to avoid aliasing.
func (q *Query[T]) clone() *Query[T] {
	q2 := *q
	// The registries are cloned too: RegisterJoin/RegisterPreloader on a
	// derived query must not reach through a shared map into the parent.
	q2.joinDefs = maps.Clone(q.joinDefs)
	q2.preloaders = maps.Clone(q.preloaders)
	q2.wheres = append([]whereClause(nil), q.wheres...)
	q2.orderBys = append([]string(nil), q.orderBys...)
	q2.joins = append([]string(nil), q.joins...)
	q2.activeJoinNames = append([]string(nil), q.activeJoinNames...)
	q2.preloads = append([]string(nil), q.preloads...)
	return &q2
}

// --- Builder methods ---

func (q *Query[T]) Where(clause string, args ...any) *Query[T] {
	q2 := q.clone()
	q2.wheres = append(q2.wheres, whereClause{clause, args})
	return q2
}

func (q *Query[T]) OrderBy(clause string) *Query[T] {
	q2 := q.clone()
	q2.orderBys = append(q2.orderBys, clause)
	return q2
}

func (q *Query[T]) Limit(n int) *Query[T] {
	q2 := q.clone()
	q2.limit = &n
	return q2
}

func (q *Query[T]) Offset(n int) *Query[T] {
	q2 := q.clone()
	q2.offset = &n
	return q2
}

func (q *Query[T]) Select(columns string) *Query[T] {
	q2 := q.clone()
	q2.selects = &columns
	return q2
}

// ForUpdate appends FOR UPDATE to the SELECT query for pessimistic locking.
func (q *Query[T]) ForUpdate() *Query[T] {
	q2 := q.clone()
	q2.forUpdate = true
	return q2
}

// ForUpdateSkipLocked appends FOR UPDATE SKIP LOCKED to the SELECT query.
// Rows locked by other transactions are skipped instead of blocking.
func (q *Query[T]) ForUpdateSkipLocked() *Query[T] {
	q2 := q.clone()
	q2.forUpdateSkipLocked = true
	return q2
}

// Join adds an INNER JOIN for the named relation.
func (q *Query[T]) Join(name string) *Query[T] {
	return q.addJoin("INNER JOIN", name)
}

// LeftJoin adds a LEFT JOIN for the named relation.
func (q *Query[T]) LeftJoin(name string) *Query[T] {
	return q.addJoin("LEFT JOIN", name)
}

func (q *Query[T]) addJoin(joinType, name string) *Query[T] {
	q2 := q.clone()
	q2.applyJoin(joinType, name)
	return q2
}

func (q *Query[T]) applyJoin(joinType, name string) {
	cfg, ok := q.joinDefs[name]
	if !ok {
		if q.err == nil {
			q.err = fmt.Errorf("orm: unknown join %q", name)
		}
		return
	}
	if slices.Contains(q.activeJoinNames, name) {
		return
	}
	// The joined table is aliased by relation name, so a self-relation and
	// two relations targeting the same table stay distinguishable.
	clause := fmt.Sprintf(
		"%s %s AS %s ON %s.%s = %s.%s",
		joinType,
		q.qi(cfg.TargetTable),
		q.qi(name),
		q.qi(name), q.qi(cfg.TargetColumn),
		q.qi(cfg.SourceTable), q.qi(cfg.SourceColumn),
	)
	q.joins = append(q.joins, clause)
	q.activeJoinNames = append(q.activeJoinNames, name)
}

// Preload registers a relation to be eagerly loaded after the main query.
func (q *Query[T]) Preload(name string) *Query[T] {
	q2 := q.clone()
	q2.preloads = append(q2.preloads, name)
	return q2
}

// Scopes applies the given scope.Scope values to the query.
func (q *Query[T]) Scopes(scopes ...scope.Scope) *Query[T] {
	q2 := q.clone()
	for _, s := range scopes {
		s.Apply(q2)
	}
	return q2
}

// --- scope.Applier implementation ---

func (q *Query[T]) ApplyWhere(clause string, args []any) {
	q.wheres = append(q.wheres, whereClause{clause, args})
}

func (q *Query[T]) ApplyOrderBy(clause string) {
	q.orderBys = append(q.orderBys, clause)
}

func (q *Query[T]) ApplyLimit(n int)  { q.limit = &n }
func (q *Query[T]) ApplyOffset(n int) { q.offset = &n }

func (q *Query[T]) ApplySelect(columns string) {
	q.selects = &columns
}

func (q *Query[T]) ApplyForUpdate()           { q.forUpdate = true }
func (q *Query[T]) ApplyForUpdateSkipLocked() { q.forUpdateSkipLocked = true }
func (q *Query[T]) ApplyJoin(name string)     { q.applyJoin("INNER JOIN", name) }
func (q *Query[T]) ApplyLeftJoin(name string) { q.applyJoin("LEFT JOIN", name) }
func (q *Query[T]) ApplyPreload(name string)  { q.preloads = append(q.preloads, name) }

var _ scope.Applier = (*Query[any])(nil)

// --- Terminal methods ---

// All executes a SELECT and returns all matching rows.
func (q *Query[T]) All(ctx context.Context) ([]T, error) {
	if q.err != nil {
		return nil, q.err
	}
	query, args := q.buildSelect()
	query, args = q.rewrite(query, args)

	rows, err := q.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err //nolint:wrapcheck // pass through
	}
	defer func() { _ = rows.Close() }()

	var result []T
	for rows.Next() {
		item, err := q.scan(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err //nolint:wrapcheck // pass through
	}

	for _, name := range q.preloads {
		fn, ok := q.preloaders[name]
		if !ok {
			return nil, fmt.Errorf("orm: unknown preload %q", name)
		}
		if err := fn(ctx, q.db, result); err != nil {
			return nil, err
		}
	}

	return result, nil
}

// First executes a SELECT with LIMIT 1 and returns the first row.
// Returns ErrNotFound if no rows match.
func (q *Query[T]) First(ctx context.Context) (T, error) {
	q2 := q.Limit(1)
	items, err := q2.All(ctx)
	if err != nil {
		var zero T
		return zero, err
	}
	if len(items) == 0 {
		var zero T
		return zero, ErrNotFound
	}
	return items[0], nil
}

// Count returns the number of rows matching the current query conditions.
func (q *Query[T]) Count(ctx context.Context) (int64, error) {
	if q.err != nil {
		return 0, q.err
	}
	query, args := q.buildCount()
	query, args = q.rewrite(query, args)

	var count int64
	rows, err := q.db.QueryContext(ctx, query, args...)
	if err != nil {
		return 0, err //nolint:wrapcheck // pass through
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return 0, errors.New("orm: COUNT returned no rows")
	}
	if err := rows.Scan(&count); err != nil {
		return 0, err //nolint:wrapcheck // pass through
	}
	return count, rows.Err() //nolint:wrapcheck // pass through
}

// Exists returns true if at least one row matches the current query conditions.
func (q *Query[T]) Exists(ctx context.Context) (bool, error) {
	if q.err != nil {
		return false, q.err
	}

	var b strings.Builder
	b.WriteString("SELECT 1 FROM ")
	b.WriteString(q.qi(q.table))
	for _, j := range q.joins {
		b.WriteByte(' ')
		b.WriteString(j)
	}
	args := q.appendWhere(&b)
	b.WriteString(" LIMIT 1")

	query := rewritePlaceholders(q.db.dialect(), b.String())
	rows, err := q.db.QueryContext(ctx, query, args...)
	if err != nil {
		return false, err //nolint:wrapcheck // pass through
	}
	defer func() { _ = rows.Close() }()
	exists := rows.Next()
	return exists, rows.Err() //nolint:wrapcheck // pass through
}

// Create inserts a new row. If setPK is set, the primary key is populated
// via RETURNING (PostgreSQL) or LastInsertId (MySQL).
func (q *Query[T]) Create(ctx context.Context, t *T) error {
	if q.err != nil {
		return q.err
	}
	q.applyTimestamps(ctx, t, true)

	includesPK := q.setPK == nil
	columns, values := q.colValPairs(t, includesPK)
	if len(columns) == 0 {
		// A default-only row has no portable INSERT: PostgreSQL wants
		// DEFAULT VALUES where MySQL wants an empty column list. A model with
		// nothing to insert is not worth that fork.
		return errors.New("orm: Create requires at least one column")
	}

	query := q.buildInsert(columns)
	query, values = q.rewrite(query, values)

	d := q.db.dialect()
	if d.UseReturning() && q.setPK != nil {
		query += d.ReturningClause(q.pk)
		rows, err := q.db.QueryContext(ctx, query, values...)
		if err != nil {
			return err //nolint:wrapcheck // pass through
		}
		defer func() { _ = rows.Close() }()
		if !rows.Next() {
			return errors.New("orm: INSERT RETURNING returned no rows")
		}
		var id int64
		if err := rows.Scan(&id); err != nil {
			return err //nolint:wrapcheck // pass through
		}
		q.setPK(t, id)
		return rows.Err() //nolint:wrapcheck // pass through
	}

	result, err := q.db.ExecContext(ctx, query, values...)
	if err != nil {
		return err //nolint:wrapcheck // pass through
	}

	if q.setPK != nil {
		id, err := result.LastInsertId()
		if err != nil {
			return err //nolint:wrapcheck // pass through
		}
		q.setPK(t, id)
	}
	return nil
}

// CreateAll inserts multiple rows in a single INSERT statement.
// If setPK is set, primary keys are populated for each row.
func (q *Query[T]) CreateAll(ctx context.Context, items []*T) error {
	if q.err != nil {
		return q.err
	}
	if len(items) == 0 {
		return nil
	}

	for _, item := range items {
		q.applyTimestamps(ctx, item, true)
	}

	includesPK := q.setPK == nil
	columns, _ := q.colValPairs(items[0], includesPK)
	if len(columns) == 0 {
		return errors.New("orm: CreateAll requires at least one column")
	}

	var allValues []any
	for _, item := range items {
		_, vals := q.colValPairs(item, includesPK)
		allValues = append(allValues, vals...)
	}

	query := q.buildBatchInsert(columns, len(items))
	query, allValues = q.rewrite(query, allValues)

	d := q.db.dialect()
	if d.UseReturning() && q.setPK != nil {
		query += d.ReturningClause(q.pk)
		rows, err := q.db.QueryContext(ctx, query, allValues...)
		if err != nil {
			return err //nolint:wrapcheck // pass through
		}
		defer func() { _ = rows.Close() }()
		n := 0
		for ; rows.Next(); n++ {
			if n >= len(items) {
				return errors.New("orm: INSERT RETURNING returned more rows than items")
			}
			var id int64
			if err := rows.Scan(&id); err != nil {
				return err //nolint:wrapcheck // pass through
			}
			q.setPK(items[n], id)
		}
		if err := rows.Err(); err != nil {
			return err //nolint:wrapcheck // pass through
		}
		// Fewer rows would leave the remaining items' keys unset while
		// reporting success.
		if n != len(items) {
			return fmt.Errorf("orm: INSERT RETURNING returned %d rows for %d items", n, len(items))
		}
		return nil
	}

	result, err := q.db.ExecContext(ctx, query, allValues...)
	if err != nil {
		return err //nolint:wrapcheck // pass through
	}

	if q.setPK != nil {
		firstID, err := result.LastInsertId()
		if err != nil {
			return err //nolint:wrapcheck // pass through
		}
		// LastInsertId is the first ID of the batch and the rest are assumed
		// consecutive, which holds for a single multi-row INSERT as long as
		// auto_increment_increment is 1.
		for i, item := range items {
			q.setPK(item, firstID+int64(i))
		}
	}
	return nil
}

// Upsert inserts a row or updates it on key conflict.
// All non-PK columns (except createdAt) are updated on conflict.
// The primary key must be set on t before calling Upsert — a zero key is
// rejected, because unlike Create nothing is read back into t on MySQL, and a
// zero auto-increment key would quietly insert a fresh row on every call.
//
// The conflict target is dialect-specific, because MySQL offers no way to name
// one: PostgreSQL's ON CONFLICT fires on the primary key alone, while MySQL's
// ON DUPLICATE KEY UPDATE fires on any unique index. A row that collides on a
// secondary unique key is therefore updated by MySQL, where PostgreSQL reports
// a constraint error.
func (q *Query[T]) Upsert(ctx context.Context, t *T) error {
	if q.err != nil {
		return q.err
	}
	q.applyTimestamps(ctx, t, true)

	columns, values := q.colValPairs(t, true) // always include PK

	pkSet := false
	for i, col := range columns {
		if col == q.pk {
			pkSet = !zeroPK(values[i])
			break
		}
	}
	if !pkSet {
		return errors.New("orm: primary key value is required for Upsert")
	}

	query := q.buildUpsert(columns)
	query, values = q.rewrite(query, values)

	d := q.db.dialect()
	if d.UseReturning() && q.setPK != nil {
		query += d.ReturningClause(q.pk)
		rows, err := q.db.QueryContext(ctx, query, values...)
		if err != nil {
			return err //nolint:wrapcheck // pass through
		}
		defer func() { _ = rows.Close() }()
		if rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				return err //nolint:wrapcheck // pass through
			}
			q.setPK(t, id)
		}
		return rows.Err() //nolint:wrapcheck // pass through
	}

	_, err := q.db.ExecContext(ctx, query, values...)
	return err //nolint:wrapcheck // pass through
}

// Update updates the row identified by the primary key of t.
// All non-PK columns are SET.
func (q *Query[T]) Update(ctx context.Context, t *T) error {
	if q.err != nil {
		return q.err
	}
	q.applyTimestamps(ctx, t, false)

	allCols, allVals := q.colValPairs(t, true)

	var setCols []string
	var setVals []any
	var pkVal any
	pkSeen := false
	for i, col := range allCols {
		switch {
		case col == q.pk:
			pkVal = allVals[i]
			pkSeen = true
		case q.isCreatedAtCol(col):
			// Create owns created_at. Setting it here would overwrite the
			// stored creation time with whatever the struct happens to hold —
			// the zero time, when the caller did not load the row first.
		default:
			setCols = append(setCols, col)
			setVals = append(setVals, allVals[i])
		}
	}
	if !pkSeen || zeroPK(pkVal) {
		return errors.New("orm: primary key value is required for Update")
	}
	if len(setCols) == 0 {
		return errors.New("orm: Update has no columns to set")
	}

	setVals = append(setVals, pkVal)
	query := q.buildUpdate(setCols)
	query, setVals = q.rewrite(query, setVals)

	_, err := q.db.ExecContext(ctx, query, setVals...)
	return err //nolint:wrapcheck // pass through
}

// Updates updates specific columns by map for rows matching the accumulated
// WHERE clauses. Returns an error if no WHERE clauses are set (safety guard).
// If updatedAt columns are registered and not present in values, they are
// automatically added with the current time.
func (q *Query[T]) Updates(ctx context.Context, values map[string]any) error {
	if q.err != nil {
		return q.err
	}
	if len(q.wheres) == 0 {
		return errors.New("orm: Updates without WHERE clause is not allowed")
	}

	// Work on a copy: the auto updated_at below must not leak into the
	// caller's map.
	vals := make(map[string]any, len(values)+len(q.updatedAtCols))
	maps.Copy(vals, values)
	if len(q.updatedAtCols) > 0 {
		n := now(ctx)
		for _, col := range q.updatedAtCols {
			if _, ok := vals[col]; !ok {
				vals[col] = n
			}
		}
	}
	if len(vals) == 0 {
		return errors.New("orm: Updates requires at least one column")
	}

	// Sorted so the same input produces the same SQL.
	setCols := slices.Sorted(maps.Keys(vals))
	setVals := make([]any, 0, len(setCols))
	for _, col := range setCols {
		setVals = append(setVals, vals[col])
	}

	var b strings.Builder
	b.WriteString(q.buildUpdateMap(setCols))
	whereArgs := q.appendWhere(&b)
	setVals = append(setVals, whereArgs...)

	query, args := q.rewrite(b.String(), setVals)

	_, err := q.db.ExecContext(ctx, query, args...)
	return err //nolint:wrapcheck // pass through
}

// Delete deletes rows matching the accumulated WHERE clauses.
// Returns an error if no WHERE clauses are set (safety guard).
func (q *Query[T]) Delete(ctx context.Context) error {
	if q.err != nil {
		return q.err
	}
	if len(q.wheres) == 0 {
		return errors.New("orm: Delete without WHERE clause is not allowed")
	}
	query, args := q.buildDelete()
	query, args = q.rewrite(query, args)

	_, err := q.db.ExecContext(ctx, query, args...)
	return err //nolint:wrapcheck // pass through
}

// --- SQL building ---

// qi quotes an identifier (table/column name) using the dialect.
func (q *Query[T]) qi(name string) string {
	return q.db.dialect().QuoteIdent(name)
}

// quoteColumns joins column names with dialect-aware quoting.
func (q *Query[T]) quoteColumns(cols []string) string {
	quoted := make([]string, len(cols))
	for i, c := range cols {
		quoted[i] = q.qi(c)
	}
	return strings.Join(quoted, ", ")
}

// qualifiedColumns returns column names qualified with the table name.
// Used when JOINs are present to avoid ambiguous column references.
func (q *Query[T]) qualifiedColumns() string {
	quoted := make([]string, len(q.columns))
	for i, c := range q.columns {
		quoted[i] = q.qi(q.table) + "." + q.qi(c)
	}
	return strings.Join(quoted, ", ")
}

func (q *Query[T]) buildSelect() (string, []any) {
	var b strings.Builder
	b.WriteString("SELECT ")

	switch {
	case q.selects != nil:
		b.WriteString(*q.selects)
	case len(q.joins) > 0:
		b.WriteString(q.qualifiedColumns())
		for _, name := range q.activeJoinNames {
			cfg := q.joinDefs[name]
			for _, col := range cfg.SelectColumns {
				b.WriteString(", ")
				b.WriteString(q.qi(name))
				b.WriteByte('.')
				b.WriteString(q.qi(col))
				b.WriteString(" AS ")
				b.WriteString(q.qi(name + "__" + col))
			}
		}
	default:
		b.WriteString(q.quoteColumns(q.columns))
	}

	b.WriteString(" FROM ")
	b.WriteString(q.qi(q.table))

	for _, j := range q.joins {
		b.WriteByte(' ')
		b.WriteString(j)
	}

	args := q.appendWhere(&b)

	if len(q.orderBys) > 0 {
		b.WriteString(" ORDER BY ")
		b.WriteString(strings.Join(q.orderBys, ", "))
	}

	if q.limit != nil {
		fmt.Fprintf(&b, " LIMIT %d", *q.limit)
	} else if q.offset != nil {
		if _, ok := q.db.dialect().(mysqlDialect); ok {
			// MySQL has no standalone OFFSET; its manual prescribes this
			// all-rows LIMIT to make one valid.
			b.WriteString(" LIMIT 18446744073709551615")
		}
	}
	if q.offset != nil {
		fmt.Fprintf(&b, " OFFSET %d", *q.offset)
	}

	if q.forUpdateSkipLocked {
		b.WriteString(" FOR UPDATE SKIP LOCKED")
	} else if q.forUpdate {
		b.WriteString(" FOR UPDATE")
	}

	return b.String(), args
}

func (q *Query[T]) buildCount() (string, []any) {
	var b strings.Builder
	// The query's LIMIT/OFFSET are deliberately not applied: a count answers
	// "how many rows match", and with an OFFSET the single COUNT row itself
	// would be skipped.
	b.WriteString("SELECT COUNT(")
	if len(q.joins) > 0 {
		// A JOIN repeats the parent row per matched child; count parents.
		b.WriteString("DISTINCT ")
		b.WriteString(q.qi(q.table))
		b.WriteByte('.')
		b.WriteString(q.qi(q.pk))
	} else {
		b.WriteByte('*')
	}
	b.WriteString(") FROM ")
	b.WriteString(q.qi(q.table))

	for _, j := range q.joins {
		b.WriteByte(' ')
		b.WriteString(j)
	}

	args := q.appendWhere(&b)

	return b.String(), args
}

func (q *Query[T]) buildInsert(columns []string) string {
	placeholders := make([]string, len(columns))
	for i := range placeholders {
		placeholders[i] = "?"
	}
	return fmt.Sprintf(
		"INSERT INTO %s (%s) VALUES (%s)",
		q.qi(q.table),
		q.quoteColumns(columns),
		strings.Join(placeholders, ", "),
	)
}

func (q *Query[T]) buildBatchInsert(columns []string, rowCount int) string {
	ph := make([]string, len(columns))
	for i := range ph {
		ph[i] = "?"
	}
	oneRow := "(" + strings.Join(ph, ", ") + ")"

	rows := make([]string, rowCount)
	for i := range rows {
		rows[i] = oneRow
	}

	return fmt.Sprintf(
		"INSERT INTO %s (%s) VALUES %s",
		q.qi(q.table),
		q.quoteColumns(columns),
		strings.Join(rows, ", "),
	)
}

func (q *Query[T]) buildUpsert(columns []string) string {
	placeholders := make([]string, len(columns))
	for i := range placeholders {
		placeholders[i] = "?"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "INSERT INTO %s (%s) VALUES (%s)",
		q.qi(q.table),
		q.quoteColumns(columns),
		strings.Join(placeholders, ", "),
	)

	var updateCols []string
	for _, col := range columns {
		if col != q.pk && !q.isCreatedAtCol(col) {
			updateCols = append(updateCols, col)
		}
	}

	d := q.db.dialect()
	if len(updateCols) == 0 {
		// Nothing to update on conflict. MySQL has no DO NOTHING, so it sets
		// the key to itself; PostgreSQL says it directly.
		if _, ok := d.(mysqlDialect); ok {
			fmt.Fprintf(&b, " ON DUPLICATE KEY UPDATE %s = %s", q.qi(q.pk), q.qi(q.pk))
		} else {
			fmt.Fprintf(&b, " ON CONFLICT (%s) DO NOTHING", q.qi(q.pk))
		}
		return b.String()
	}
	if _, ok := d.(mysqlDialect); ok {
		sets := make([]string, len(updateCols))
		for i, col := range updateCols {
			sets[i] = fmt.Sprintf("%s = VALUES(%s)", q.qi(col), q.qi(col))
		}
		fmt.Fprintf(&b, " ON DUPLICATE KEY UPDATE %s", strings.Join(sets, ", "))
	} else {
		sets := make([]string, len(updateCols))
		for i, col := range updateCols {
			sets[i] = fmt.Sprintf("%s = EXCLUDED.%s", q.qi(col), q.qi(col))
		}
		fmt.Fprintf(&b, " ON CONFLICT (%s) DO UPDATE SET %s", q.qi(q.pk), strings.Join(sets, ", "))
	}

	return b.String()
}

func (q *Query[T]) buildUpdate(setCols []string) string {
	sets := make([]string, len(setCols))
	for i, col := range setCols {
		sets[i] = q.qi(col) + " = ?"
	}
	return fmt.Sprintf(
		"UPDATE %s SET %s WHERE %s = ?",
		q.qi(q.table),
		strings.Join(sets, ", "),
		q.qi(q.pk),
	)
}

func (q *Query[T]) buildUpdateMap(setCols []string) string {
	sets := make([]string, len(setCols))
	for i, col := range setCols {
		sets[i] = q.qi(col) + " = ?"
	}
	return fmt.Sprintf(
		"UPDATE %s SET %s",
		q.qi(q.table),
		strings.Join(sets, ", "),
	)
}

func (q *Query[T]) buildDelete() (string, []any) {
	var b strings.Builder
	b.WriteString("DELETE FROM ")
	b.WriteString(q.qi(q.table))
	args := q.appendWhere(&b)
	return b.String(), args
}

func (q *Query[T]) appendWhere(b *strings.Builder) []any {
	if len(q.wheres) == 0 {
		return nil
	}

	var args []any
	b.WriteString(" WHERE ")
	for i, w := range q.wheres {
		if i > 0 {
			b.WriteString(" AND ")
		}
		b.WriteString(w.clause)
		args = append(args, w.args...)
	}
	return args
}

// rewrite converts ? placeholders to dialect-specific placeholders.
// For MySQL this is a no-op. For PostgreSQL, ? becomes $1, $2, etc.
func (q *Query[T]) rewrite(query string, args []any) (string, []any) {
	return rewritePlaceholders(q.db.dialect(), query), args
}

// zeroPK reports whether a primary key value is absent. The type switch covers
// the key types generated code emits; reflection stays out of the runtime.
func zeroPK(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case int:
		return x == 0
	case int8:
		return x == 0
	case int16:
		return x == 0
	case int32:
		return x == 0
	case int64:
		return x == 0
	case uint:
		return x == 0
	case uint8:
		return x == 0
	case uint16:
		return x == 0
	case uint32:
		return x == 0
	case uint64:
		return x == 0
	case string:
		return x == ""
	default:
		return false
	}
}

// applyTimestamps sets createdAt and/or updatedAt on t using the Clock
// from ctx (or time.Now). When isCreate is false, only updatedAt is set.
func (q *Query[T]) applyTimestamps(ctx context.Context, t *T, isCreate bool) {
	if q.setCreatedAt == nil && q.setUpdatedAt == nil {
		return
	}
	n := now(ctx)
	if isCreate && q.setCreatedAt != nil {
		q.setCreatedAt(t, n)
	}
	if q.setUpdatedAt != nil {
		q.setUpdatedAt(t, n)
	}
}

func (q *Query[T]) isCreatedAtCol(col string) bool {
	return slices.Contains(q.createdAtCols, col)
}
