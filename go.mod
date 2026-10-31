module github.com/go-kanna/kanna

go 1.26.0

toolchain go1.26.9

require (
	github.com/go-kanna/internal v0.0.16
	golang.org/x/mod v0.42.0
)

require (
	golang.org/x/sync v0.24.0 // indirect
	golang.org/x/tools v0.52.0 // indirect
)

// Another module, kanna v0.2.x with the first kanna-orm, which this path used to hold.
retract [v0.0.1, v0.2.0]
