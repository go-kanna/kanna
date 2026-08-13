// Package orm generates type-safe query code from annotated model structs.
package orm

import (
	"github.com/jinzhu/inflection"

	"github.com/go-kanna/kanna/internal/relation"
)

// tableName converts a CamelCase type name to a snake_case plural table name:
// "User" → "users", "UserProfile" → "user_profiles".
func tableName(typeName string) string {
	return inflection.Plural(relation.SnakeCase(typeName))
}

// factoryName is the generated factory's identifier: the struct name
// pluralized in place ("User" → "Users", "OAuthClient" → "OAuthClients").
// Deriving it from the struct name rather than the table name keeps Go
// identifiers out of the snake_case round-trip that would need an initialism
// dictionary to survive, and keeps a name= override from renaming Go API.
func factoryName(structName string) string {
	return inflection.Plural(structName)
}
