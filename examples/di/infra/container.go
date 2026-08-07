package infra

// Deps groups the infrastructure dependencies so an application container can
// take all of them through a single di:"embed" field instead of listing each
// one.
//
// NewDB can fail, so the generated NewDeps returns (Deps, error). The must
// directive additionally emits MustNewDeps, which panics on that error — handy
// at the top of main, where there is nothing to hand the error to.
//
// Without returns= a container is constructed as a pointer. Deps only bundles a
// couple of pointers and carries no identity of its own, so returning it by
// value costs nothing and spares callers a nil check.
//
//kanna:container must returns=Deps
type Deps struct {
	DB    *DB    `di:""`
	Cache *Cache `di:""`
}
