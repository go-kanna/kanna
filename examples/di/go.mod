module github.com/go-kanna/kanna/examples/di

go 1.25.0

// There is no matching require yet: kanna is unreleased, so this resolves
// through the repository's go.work. Once it carries a tag, running
// `go get -tool github.com/go-kanna/kanna/cmd/kanna-di` in your own module adds
// the require and the setup stands on its own.
tool (
	github.com/go-kanna/kanna/cmd/kanna-di
)
