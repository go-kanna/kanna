module github.com/go-kanna/kanna/examples/i18n

go 1.25.0

// There is no matching require yet: kanna is unreleased, so this resolves
// through the repository's go.work. Once it carries a tag, running
// `go get -tool github.com/go-kanna/kanna/cmd/kanna-i18n` in your own module
// adds the require and the setup stands on its own.
tool github.com/go-kanna/kanna/cmd/kanna-i18n

require (
	github.com/go-kanna/kanna v0.0.1
	golang.org/x/text v0.41.0
)

require (
	github.com/kr/text v0.2.0 // indirect
	github.com/pelletier/go-toml/v2 v2.4.3 // indirect
	github.com/rogpeppe/go-internal v1.6.1 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
