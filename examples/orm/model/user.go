package model

import "time"

//kanna:table
type User struct {
	ID        int
	Name      string
	Email     string
	CreatedAt time.Time
	Posts     []Post   `orm:"has_many,foreign_key:user_id"`
	Profile   *Profile `orm:"has_one,foreign_key:user_id"`
	Tags      []Tag    `orm:"many_to_many,join_table:user_tags,foreign_key:user_id,references:tag_id"`
}
