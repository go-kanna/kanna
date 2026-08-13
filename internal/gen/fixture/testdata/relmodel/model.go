// Package relmodel exercises graph generation: a chain of required
// belongs_to relations, an optional self reference that must stay out of the
// graph, and two parents of one table sharing a primary-key space.
package relmodel

//kanna:table
type Company struct {
	ID   int64
	Name string
}

//kanna:table
type Department struct {
	ID        int64
	CompanyID int64
	Company   *Company `orm:"belongs_to,foreign_key:company_id"`
	Name      string
}

//kanna:table
type Employee struct {
	ID           int64
	DepartmentID int64
	Department   *Department `orm:"belongs_to,foreign_key:department_id"`
	ManagerID    *int64
	Manager      *Employee `orm:"belongs_to,foreign_key:manager_id"`
	Name         string
}

//kanna:table
type User struct {
	ID   string
	Name string
}

//kanna:table
type Post struct {
	ID       int64
	AuthorID string
	Author   User `orm:"belongs_to,foreign_key:author_id"`
	EditorID string
	Editor   User `orm:"belongs_to,foreign_key:editor_id"`
	Title    string
}
