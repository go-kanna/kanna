package i18n

// Arg is a named argument of a Message.
type Arg struct {
	Name  string
	Value any
}

// Message identifies a localizable message and carries its arguments.
// Messages are usually constructed by code generated with kanna-i18n rather
// than by hand.
type Message struct {
	Key  string
	Args []Arg
}
