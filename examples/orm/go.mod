module github.com/go-kanna/kanna/examples/orm

go 1.25.0

tool (
	github.com/go-kanna/kanna/cmd/kanna-orm
)

require (
	github.com/go-sql-driver/mysql v1.10.0
	github.com/jackc/pgx/v5 v5.10.0
)
