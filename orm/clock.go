package orm

import (
	"context"
	"time"
)

// Clock provides the current time. Implementations can return fixed
// times for deterministic testing.
type Clock interface {
	Now() time.Time
}

type clockKey struct{}

// WithClock returns a child context carrying the given Clock.
// Query methods (Create, Update, Upsert) will use this Clock
// instead of time.Now() when auto-setting timestamp fields.
func WithClock(ctx context.Context, c Clock) context.Context {
	return context.WithValue(ctx, clockKey{}, c)
}

// now returns the current time from the Clock in ctx, or time.Now()
// if no Clock is present.
func now(ctx context.Context) time.Time {
	if c, ok := ctx.Value(clockKey{}).(Clock); ok {
		return c.Now()
	}
	return time.Now()
}
