// Package infra holds the infrastructure dependencies of the example
// application, plus the providers that construct them.
//
// A provider is any top-level function returning (T) or (T, error). kanna-di
// finds them by scanning the packages you point it at — nothing needs to be
// registered.
package infra

import "errors"

// Config carries the settings the rest of the infrastructure needs.
type Config struct {
	DSN string
}

// NewConfig provides a Config.
func NewConfig() Config {
	return Config{DSN: "postgres://localhost/example"}
}

// DB stands in for a database handle.
type DB struct {
	dsn string
}

// NewDB provides a *DB, and may fail. Any constructor that ends up calling it
// returns an error too.
func NewDB(c Config) (*DB, error) {
	if c.DSN == "" {
		return nil, errors.New("infra: config has no DSN")
	}
	return &DB{dsn: c.DSN}, nil
}

// DSN reports the connection string the handle was opened with.
func (db *DB) DSN() string {
	return db.dsn
}

// Cache stands in for a cache client.
type Cache struct{}

// NewCache provides a *Cache.
func NewCache() *Cache {
	return &Cache{}
}
