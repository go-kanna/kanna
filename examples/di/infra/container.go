package infra

// Deps groups the infrastructure dependencies so an application container can
// take all of them through a single di:"embed" field instead of listing each
// one.
//
// NewDB can fail, so the generated NewDeps returns (*Deps, error). The must
// directive additionally emits MustNewDeps, which panics on that error — handy
// at the top of main, where there is nothing to hand the error to.
//
//kanna:container must
type Deps struct {
	DB    *DB    `di:""`
	Cache *Cache `di:""`
}
