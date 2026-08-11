package model

//kanna:table
type Post struct {
	ID     int
	UserID int
	Title  string
	Body   string
	User   *User `orm:"belongs_to,foreign_key:user_id"`
}
