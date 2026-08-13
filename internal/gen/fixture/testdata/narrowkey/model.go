// Package narrowkey uses integer keys too narrow for the shared counter,
// which would wrap: they stay off it, unique only within a graph.
package narrowkey

//kanna:table
type Ticket struct {
	ID   uint8
	Code string
}

//kanna:table
type Stub struct {
	ID       int64
	TicketID uint8
	Ticket   *Ticket `orm:"belongs_to,foreign_key:ticket_id"`
}

//kanna:table
type Pair struct {
	ID      int64
	AlphaID uint8
	Alpha   *Ticket `orm:"belongs_to,foreign_key:alpha_id"`
	BetaID  uint8
	Beta    *Ticket `orm:"belongs_to,foreign_key:beta_id"`
}
