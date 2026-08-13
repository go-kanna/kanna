// Package collide declares a struct whose fixture takes the name the
// Department graph would want.
package collide

//kanna:table
type Company struct {
	ID int64
}

//kanna:table
type Department struct {
	ID        int64
	CompanyID int64
	Company   *Company `orm:"belongs_to,foreign_key:company_id"`
}

type DepartmentGraph struct {
	Value int
}
