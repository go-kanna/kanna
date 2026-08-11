package orm

import (
	"context"
	"fmt"
	"strings"
)

// JoinPair holds a source–target pair read from a join table.
type JoinPair[S, T comparable] struct {
	Source S
	Target T
}

// QueryJoinTable reads (sourceCol, targetCol) rows from the given join table
// where sourceCol IN (sourceIDs). It returns a slice of JoinPair.
func QueryJoinTable[S, T comparable](
	ctx context.Context, db Querier, table, sourceCol, targetCol string, sourceIDs []S,
) ([]JoinPair[S, T], error) {
	if len(sourceIDs) == 0 {
		return nil, nil
	}

	d := db.dialect()
	qi := d.QuoteIdent

	placeholders := make([]string, len(sourceIDs))
	args := make([]any, len(sourceIDs))
	for i, id := range sourceIDs {
		placeholders[i] = "?"
		args[i] = id
	}

	query := fmt.Sprintf(
		"SELECT %s, %s FROM %s WHERE %s IN (%s)",
		qi(sourceCol), qi(targetCol), qi(table), qi(sourceCol),
		strings.Join(placeholders, ", "),
	)

	query = rewritePlaceholders(d, query)

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err //nolint:wrapcheck // pass through
	}
	defer func() { _ = rows.Close() }()

	var pairs []JoinPair[S, T]
	for rows.Next() {
		var p JoinPair[S, T]
		if err := rows.Scan(&p.Source, &p.Target); err != nil {
			return nil, err //nolint:wrapcheck // pass through
		}
		pairs = append(pairs, p)
	}
	return pairs, rows.Err() //nolint:wrapcheck // pass through
}

// UniqueTargets extracts deduplicated target values from a slice of JoinPair.
func UniqueTargets[S, T comparable](pairs []JoinPair[S, T]) []T {
	seen := make(map[T]struct{}, len(pairs))
	result := make([]T, 0, len(pairs))
	for _, p := range pairs {
		if _, ok := seen[p.Target]; !ok {
			seen[p.Target] = struct{}{}
			result = append(result, p.Target)
		}
	}
	return result
}

// GroupBySource groups JoinPair values by source key into a map[S][]T.
func GroupBySource[S, T comparable](pairs []JoinPair[S, T]) map[S][]T {
	m := make(map[S][]T)
	for _, p := range pairs {
		m[p.Source] = append(m[p.Source], p.Target)
	}
	return m
}
