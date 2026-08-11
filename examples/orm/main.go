// Command orm drives the generated query code end to end against a real
// database: CRUD, scopes, joins, the three preload kinds, transactions, and
// upserts. It expects the containers from orm/integration/compose.yaml and
// prints how to start them when they are not running.
//
// Regenerate with:
//
//	go generate ./...
//
//go:generate go tool kanna-orm -source ./model -destination ./query
//nolint:gocyclo,perfsprint,wrapcheck,lll,maintidx // example code
package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log"
	"time"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/go-kanna/kanna/examples/orm/model"
	"github.com/go-kanna/kanna/examples/orm/query"
	"github.com/go-kanna/kanna/examples/orm/repo"
	"github.com/go-kanna/kanna/orm"
	"github.com/go-kanna/kanna/orm/scope"
)

var createTablesMySQL = []string{
	`CREATE TABLE users (
		id INT AUTO_INCREMENT PRIMARY KEY,
		name VARCHAR(255) NOT NULL,
		email VARCHAR(255) NOT NULL,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`,
	`CREATE TABLE posts (
		id INT AUTO_INCREMENT PRIMARY KEY,
		user_id INT NOT NULL,
		title VARCHAR(255) NOT NULL,
		body TEXT NOT NULL,
		FOREIGN KEY (user_id) REFERENCES users(id)
	)`,
	`CREATE TABLE profiles (
		id INT AUTO_INCREMENT PRIMARY KEY,
		user_id INT NOT NULL UNIQUE,
		bio TEXT NOT NULL,
		FOREIGN KEY (user_id) REFERENCES users(id)
	)`,
	`CREATE TABLE tags (
		id INT AUTO_INCREMENT PRIMARY KEY,
		name VARCHAR(255) NOT NULL
	)`,
	`CREATE TABLE user_tags (
		user_id INT NOT NULL,
		tag_id INT NOT NULL,
		PRIMARY KEY (user_id, tag_id),
		FOREIGN KEY (user_id) REFERENCES users(id),
		FOREIGN KEY (tag_id) REFERENCES tags(id)
	)`,
}

var createTablesPostgreSQL = []string{
	`CREATE TABLE users (
		id SERIAL PRIMARY KEY,
		name VARCHAR(255) NOT NULL,
		email VARCHAR(255) NOT NULL,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`,
	`CREATE TABLE posts (
		id SERIAL PRIMARY KEY,
		user_id INT NOT NULL REFERENCES users(id),
		title VARCHAR(255) NOT NULL,
		body TEXT NOT NULL
	)`,
	`CREATE TABLE profiles (
		id SERIAL PRIMARY KEY,
		user_id INT NOT NULL UNIQUE REFERENCES users(id),
		bio TEXT NOT NULL
	)`,
	`CREATE TABLE tags (
		id SERIAL PRIMARY KEY,
		name VARCHAR(255) NOT NULL
	)`,
	`CREATE TABLE user_tags (
		user_id INT NOT NULL REFERENCES users(id),
		tag_id INT NOT NULL REFERENCES tags(id),
		PRIMARY KEY (user_id, tag_id)
	)`,
}

func main() {
	dialect := flag.String("dialect", "mysql", "database dialect (mysql or postgres)")
	flag.Parse()

	ctx := context.Background()

	db, createTableSQLs := openDB(ctx, *dialect)
	if db == nil {
		fmt.Println("databases are not running; start them with:")
		fmt.Println("  docker compose -f ../../orm/integration/compose.yaml up -d --wait")
		return
	}

	// CREATE TABLE
	fmt.Println("--- CREATE TABLE ---")
	for _, table := range []string{"user_tags", "tags", "profiles", "posts", "users"} {
		if _, err := db.ExecContext(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", table)); err != nil {
			log.Fatalf("drop: %v", err)
		}
	}
	for _, ddl := range createTableSQLs {
		if _, err := db.ExecContext(ctx, ddl); err != nil {
			log.Fatalf("create table: %v", err)
		}
	}
	fmt.Println("Tables 'users' and 'posts' created.")

	userRepo := repo.NewUserRepository(db)

	// INSERT
	fmt.Println("\n--- INSERT ---")
	now := time.Now()
	names := []string{"Alice", "Bob", "Charlie", "Diana", "Eve"}
	users := make([]*model.User, len(names))
	for i, name := range names {
		u := &model.User{
			Name:      name,
			Email:     fmt.Sprintf("%s@example.com", name),
			CreatedAt: now,
		}
		if err := userRepo.Create(ctx, u); err != nil {
			log.Fatalf("create %s: %v", name, err)
		}
		users[i] = u
		fmt.Printf("Created: ID=%d Name=%s\n", u.ID, u.Name)
	}

	// SELECT (all)
	fmt.Println("\n--- SELECT ALL ---")
	all, err := userRepo.FindAll(ctx)
	if err != nil {
		log.Fatalf("find all: %v", err)
	}
	for _, u := range all {
		fmt.Printf("  ID=%d Name=%s Email=%s\n", u.ID, u.Name, u.Email)
	}

	// SELECT (by ID)
	fmt.Println("\n--- SELECT BY ID ---")
	found, err := userRepo.FindByID(ctx, users[0].ID)
	if err != nil {
		log.Fatalf("find by ID: %v", err)
	}
	fmt.Printf("Found: ID=%d Name=%s\n", found.ID, found.Name)

	// UPDATE
	fmt.Println("\n--- UPDATE ---")
	users[0].Name = "Alice Updated"
	users[0].Email = "alice.updated@example.com"
	if err := userRepo.Update(ctx, users[0]); err != nil {
		log.Fatalf("update: %v", err)
	}
	updated, err := userRepo.FindByID(ctx, users[0].ID)
	if err != nil {
		log.Fatalf("find after update: %v", err)
	}
	fmt.Printf("Updated: ID=%d Name=%s Email=%s\n", updated.ID, updated.Name, updated.Email)

	// DELETE
	fmt.Println("\n--- DELETE ---")
	if err := userRepo.Delete(ctx, users[1].ID); err != nil {
		log.Fatalf("delete: %v", err)
	}
	fmt.Printf("Deleted user with ID=%d\n", users[1].ID)

	// SCOPES
	fmt.Println("\n--- SCOPES ---")

	// Paginate with Limit + Offset
	fmt.Println("Paginate (limit=2, offset=1):")
	paginate := scope.Scopes{scope.Limit(2), scope.Offset(1)}
	page, err := userRepo.FindAll(ctx, paginate...)
	if err != nil {
		log.Fatalf("paginate: %v", err)
	}
	for _, u := range page {
		fmt.Printf("  ID=%d Name=%s\n", u.ID, u.Name)
	}

	// Filter with Where
	fmt.Println("Where (name LIKE 'A%%'):")
	filtered, err := userRepo.FindAll(ctx, scope.Where("name LIKE ?", "A%"))
	if err != nil {
		log.Fatalf("where: %v", err)
	}
	for _, u := range filtered {
		fmt.Printf("  ID=%d Name=%s\n", u.ID, u.Name)
	}

	// Filter with In
	fmt.Println("In (id IN ...):")
	ids := []int{users[0].ID, users[2].ID, users[4].ID}
	inResult, err := userRepo.FindAll(ctx, scope.In("id", ids))
	if err != nil {
		log.Fatalf("in: %v", err)
	}
	for _, u := range inResult {
		fmt.Printf("  ID=%d Name=%s\n", u.ID, u.Name)
	}

	// Create posts for Join/Preload demo
	fmt.Println("\n--- INSERT POSTS ---")
	postData := []struct {
		userIdx int
		title   string
	}{
		{0, "Alice's first post"},
		{0, "Alice's second post"},
		{2, "Charlie's post"},
		{4, "Eve's post"},
	}
	for _, pd := range postData {
		p := &model.Post{
			UserID: users[pd.userIdx].ID,
			Title:  pd.title,
			Body:   "body of " + pd.title,
		}
		if err := query.Posts(db).Create(ctx, p); err != nil {
			log.Fatalf("create post: %v", err)
		}
		fmt.Printf("Created: ID=%d UserID=%d Title=%q\n", p.ID, p.UserID, p.Title)
	}

	// JOIN
	fmt.Println("\n--- JOIN ---")
	fmt.Println("Users who have posts (INNER JOIN):")
	joined, err := query.Users(db).Join("Posts").Select("DISTINCT users.*").All(ctx)
	if err != nil {
		log.Fatalf("join: %v", err)
	}
	for _, u := range joined {
		fmt.Printf("  ID=%d Name=%s\n", u.ID, u.Name)
	}

	// PRELOAD (has_many)
	fmt.Println("\n--- PRELOAD (has_many) ---")
	fmt.Println("Users with their posts:")
	usersWithPosts, err := query.Users(db).Preload("Posts").OrderBy("id").All(ctx)
	if err != nil {
		log.Fatalf("preload: %v", err)
	}
	for _, u := range usersWithPosts {
		fmt.Printf("  User: ID=%d Name=%s (posts: %d)\n", u.ID, u.Name, len(u.Posts))
		for _, p := range u.Posts {
			fmt.Printf("    Post: ID=%d Title=%q\n", p.ID, p.Title)
		}
	}

	// PRELOAD (belongs_to)
	fmt.Println("\n--- PRELOAD (belongs_to) ---")
	fmt.Println("Posts with their author:")
	postsWithUser, err := query.Posts(db).Preload("User").OrderBy("id").All(ctx)
	if err != nil {
		log.Fatalf("preload: %v", err)
	}
	for _, p := range postsWithUser {
		fmt.Printf("  Post: ID=%d Title=%q Author=%s\n", p.ID, p.Title, p.User.Name)
	}

	// TRANSACTION (commit)
	fmt.Println("\n--- TRANSACTION (commit) ---")
	err = db.Transaction(ctx, func(tx orm.Querier) error {
		u := &model.User{Name: "TxUser", Email: "tx@example.com", CreatedAt: time.Now()}
		if err := query.Users(tx).Create(ctx, u); err != nil {
			return err
		}
		p := &model.Post{UserID: u.ID, Title: "TxUser's post", Body: "created in transaction"}
		if err := query.Posts(tx).Create(ctx, p); err != nil {
			return err
		}
		fmt.Printf("  In tx: created User ID=%d and Post ID=%d\n", u.ID, p.ID)
		return nil // → commit
	})
	if err != nil {
		log.Fatalf("transaction commit: %v", err)
	}
	fmt.Println("  Transaction committed successfully.")

	// TRANSACTION (rollback)
	fmt.Println("\n--- TRANSACTION (rollback) ---")
	err = db.Transaction(ctx, func(tx orm.Querier) error {
		u := &model.User{Name: "GhostUser", Email: "ghost@example.com", CreatedAt: time.Now()}
		if err := query.Users(tx).Create(ctx, u); err != nil {
			return err
		}
		fmt.Printf("  In tx: created User ID=%d (will be rolled back)\n", u.ID)
		return errors.New("intentional error")
	})
	fmt.Printf("  Transaction returned error: %v\n", err)

	// Verify rollback
	_, err = query.Users(db).Where("name = ?", "GhostUser").First(ctx)
	if err != nil {
		fmt.Println("  GhostUser not found — rollback confirmed.")
	}

	// COUNT
	fmt.Println("\n--- COUNT ---")
	totalUsers, err := query.Users(db).Count(ctx)
	if err != nil {
		log.Fatalf("count: %v", err)
	}
	fmt.Printf("  Total users: %d\n", totalUsers)

	aCount, err := query.Users(db).Where("name LIKE ?", "A%").Count(ctx)
	if err != nil {
		log.Fatalf("count where: %v", err)
	}
	fmt.Printf("  Users with name starting with 'A': %d\n", aCount)

	// EXISTS
	fmt.Println("\n--- EXISTS ---")
	exists, err := query.Users(db).Where("email = ?", "alice.updated@example.com").Exists(ctx)
	if err != nil {
		log.Fatalf("exists: %v", err)
	}
	fmt.Printf("  alice.updated@example.com exists: %v\n", exists)

	notExists, err := query.Users(db).Where("email = ?", "nobody@example.com").Exists(ctx)
	if err != nil {
		log.Fatalf("exists: %v", err)
	}
	fmt.Printf("  nobody@example.com exists: %v\n", notExists)

	// BATCH INSERT
	fmt.Println("\n--- BATCH INSERT ---")
	batchPosts := []*model.Post{
		{UserID: users[0].ID, Title: "Batch post 1", Body: "body 1"},
		{UserID: users[2].ID, Title: "Batch post 2", Body: "body 2"},
		{UserID: users[4].ID, Title: "Batch post 3", Body: "body 3"},
	}
	if err := query.Posts(db).CreateAll(ctx, batchPosts); err != nil {
		log.Fatalf("batch insert: %v", err)
	}
	for _, p := range batchPosts {
		fmt.Printf("  Created: ID=%d UserID=%d Title=%q\n", p.ID, p.UserID, p.Title)
	}

	// UPSERT
	fmt.Println("\n--- UPSERT ---")
	upsertPost := &model.Post{ID: batchPosts[0].ID, UserID: users[0].ID, Title: "Batch post 1 (upserted)", Body: "updated body"}
	if err := query.Posts(db).Upsert(ctx, upsertPost); err != nil {
		log.Fatalf("upsert: %v", err)
	}
	got, err := query.Posts(db).Where("id = ?", batchPosts[0].ID).First(ctx)
	if err != nil {
		log.Fatalf("find after upsert: %v", err)
	}
	fmt.Printf("  After upsert: ID=%d Title=%q Body=%q\n", got.ID, got.Title, got.Body)

	// PRELOAD (has_one)
	fmt.Println("\n--- PRELOAD (has_one) ---")
	profileData := []struct {
		userIdx int
		bio     string
	}{
		{0, "Alice's bio"},
		{2, "Charlie's bio"},
		{4, "Eve's bio"},
	}
	for _, pd := range profileData {
		p := &model.Profile{UserID: users[pd.userIdx].ID, Bio: pd.bio}
		if err := query.Profiles(db).Create(ctx, p); err != nil {
			log.Fatalf("create profile: %v", err)
		}
	}
	usersWithProfile, err := query.Users(db).Preload("Profile").OrderBy("id").All(ctx)
	if err != nil {
		log.Fatalf("preload profile: %v", err)
	}
	for _, u := range usersWithProfile {
		if u.Profile != nil {
			fmt.Printf("  User: ID=%d Name=%s Bio=%q\n", u.ID, u.Name, u.Profile.Bio)
		} else {
			fmt.Printf("  User: ID=%d Name=%s (no profile)\n", u.ID, u.Name)
		}
	}

	// PRELOAD (many_to_many)
	fmt.Println("\n--- PRELOAD (many_to_many) ---")
	tagNames := []string{"Go", "ORM", "SQL"}
	tags := make([]*model.Tag, len(tagNames))
	for i, name := range tagNames {
		t := &model.Tag{Name: name}
		if err := query.Tags(db).Create(ctx, t); err != nil {
			log.Fatalf("create tag: %v", err)
		}
		tags[i] = t
		fmt.Printf("Created tag: ID=%d Name=%s\n", t.ID, t.Name)
	}

	// Associate tags with users via user_tags join table
	userTagPairs := []struct {
		userIdx int
		tagIdx  int
	}{
		{0, 0}, {0, 1}, {0, 2}, // Alice: Go, ORM, SQL
		{2, 0}, {2, 2}, // Charlie: Go, SQL
		{4, 1}, // Eve: ORM
	}
	for _, p := range userTagPairs {
		if _, err := db.ExecContext(ctx,
			fmt.Sprintf("INSERT INTO user_tags (user_id, tag_id) VALUES (%d, %d)", users[p.userIdx].ID, tags[p.tagIdx].ID),
		); err != nil {
			log.Fatalf("insert user_tag: %v", err)
		}
	}

	usersWithTags, err := query.Users(db).Preload("Tags").OrderBy("id").All(ctx)
	if err != nil {
		log.Fatalf("preload tags: %v", err)
	}
	for _, u := range usersWithTags {
		tagStrs := make([]string, len(u.Tags))
		for i, t := range u.Tags {
			tagStrs[i] = t.Name
		}
		fmt.Printf("  User: ID=%d Name=%s Tags=%v\n", u.ID, u.Name, tagStrs)
	}
}

// openDB connects for the chosen dialect, or returns nil when the database is
// not reachable so the example can say how to start it instead of crashing.
func openDB(ctx context.Context, dialect string) (*orm.DB, []string) {
	var (
		sqlDB  *sql.DB
		err    error
		d      orm.Dialect
		tables []string
	)

	switch dialect {
	case "mysql":
		sqlDB, err = sql.Open("mysql", "root:root@tcp(127.0.0.1:3306)/kanna_test?parseTime=true")
		d, tables = orm.MySQL, createTablesMySQL
	case "postgres":
		sqlDB, err = sql.Open("pgx", "postgres://postgres:postgres@127.0.0.1:5432/kanna_test?sslmode=disable")
		d, tables = orm.PostgreSQL, createTablesPostgreSQL
	default:
		log.Fatalf("unknown dialect: %s (use 'mysql' or 'postgres')", dialect)
	}
	if err != nil {
		log.Fatalf("open %s: %v", dialect, err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := sqlDB.PingContext(pingCtx); err != nil {
		return nil, nil
	}

	return orm.New(sqlDB, d), tables
}
