// Package i18n renders the localized messages that kanna-i18n compiles into
// generated code.
//
// The generator reads a directory of locale files, validates every language
// against the default one, and emits a package holding typed message
// constructors together with the translations themselves. Nothing is parsed at
// run time; what remains here is what depends on run-time values — choosing a
// language, selecting a CLDR plural form, formatting numbers by locale — and
// none of it is reflection over the caller's types beyond the message
// arguments it is handed.
//
// This package is one of the few kanna publishes: both the generated code and
// the code calling Localize import it.
package i18n
