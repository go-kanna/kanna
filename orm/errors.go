package orm

import "errors"

// ErrNotFound is returned when a query expects exactly one row but finds none.
var ErrNotFound = errors.New("orm: not found")
