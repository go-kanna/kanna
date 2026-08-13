// Package badtags carries a malformed orm tag: fixtures must still generate,
// with the graph machinery reduced to a warning.
package badtags

//kanna:table
type Widget struct {
	ID   int64
	Name string `orm:"name,unique"`
}

type Plain struct {
	ID int64
}
