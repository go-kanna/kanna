module github.com/go-kanna/kanna/examples/i18n

go 1.25.0

// There is no matching require yet: kanna is unreleased, so this resolves
// through the repository's go.work. Once it carries a tag, running
// `go get -tool github.com/go-kanna/kanna/cmd/kanna-i18n` in your own module
// adds the require and the setup stands on its own.
tool (
	github.com/go-kanna/kanna/cmd/kanna-i18n
)

require golang.org/x/text v0.39.0
