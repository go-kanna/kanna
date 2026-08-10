package orm

// TableNamer can be implemented by model structs to override the
// auto-derived table name.
type TableNamer interface {
	TableName() string
}

// ResolveTableName returns the table name for type T.
// If T implements TableNamer (value or pointer receiver), that name is used;
// otherwise fallback is returned.
func ResolveTableName[T any](fallback string) string {
	var zero T
	if tn, ok := any(&zero).(TableNamer); ok {
		return tn.TableName()
	}
	return fallback
}
