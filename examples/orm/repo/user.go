//nolint:wrapcheck // example code
package repo

import (
	"context"

	"github.com/go-kanna/kanna/examples/orm/model"
	"github.com/go-kanna/kanna/examples/orm/query"
	"github.com/go-kanna/kanna/orm"
	"github.com/go-kanna/kanna/orm/scope"
)

// UserRepository wraps generated query functions with a repository pattern.
type UserRepository struct {
	db orm.Querier
}

func NewUserRepository(db orm.Querier) *UserRepository {
	return &UserRepository{db: db}
}

func (r *UserRepository) Create(ctx context.Context, u *model.User) error {
	return query.Users(r.db).Create(ctx, u)
}

func (r *UserRepository) FindByID(ctx context.Context, id int) (model.User, error) {
	return query.Users(r.db).Where("id = ?", id).First(ctx)
}

func (r *UserRepository) FindAll(ctx context.Context, scopes ...scope.Scope) ([]model.User, error) {
	return query.Users(r.db).Scopes(scopes...).OrderBy("id").All(ctx)
}

func (r *UserRepository) Update(ctx context.Context, u *model.User) error {
	return query.Users(r.db).Update(ctx, u)
}

func (r *UserRepository) Delete(ctx context.Context, id int) error {
	return query.Users(r.db).Where("id = ?", id).Delete(ctx)
}
