package main

import (
	"slices"
	"testing"
)

func TestBuildTags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		flags string
		want  []string
	}{
		{flags: ""},
		{flags: "-mod=readonly -tags=foo,bar", want: []string{"foo", "bar"}},
		{flags: "--tags=foo,,bar,", want: []string{"foo", "bar"}},
		{flags: "'-tags=foo bar'", want: []string{"foo", "bar"}},
		{flags: `"-tags=foo bar"`, want: []string{"foo", "bar"}},
		{flags: "-tags=old -tags=new", want: []string{"new"}},
		{flags: "-tags=old -tags="},
		{flags: `'-ldflags=-X main.text=-tags=wrong' -tags=right`, want: []string{"right"}},
	}
	for _, tt := range tests {
		t.Run(tt.flags, func(t *testing.T) {
			t.Parallel()

			got, err := buildTags(tt.flags)
			if err != nil || !slices.Equal(got, tt.want) {
				t.Fatalf("buildTags(%q) = %q, %v; want %q", tt.flags, got, err, tt.want)
			}
		})
	}
}

func TestBuildTags_Invalid(t *testing.T) {
	t.Parallel()

	for _, flags := range []string{"-tags", "--tags", "'-tags=foo", `"-tags=foo`} {
		t.Run(flags, func(t *testing.T) {
			t.Parallel()

			if tags, err := buildTags(flags); err == nil {
				t.Fatalf("buildTags(%q) = %q without an error", flags, tags)
			}
		})
	}
}

func TestGen_GOFLAGSBuildTags(t *testing.T) {
	t.Setenv("GOFLAGS", "-tags=kannatest")

	dir := project(t, map[string]string{
		"a/a.go":      "package a\n\n//go:generate go tool kanna-fake -out a_gen.go\n",
		"a/tagged.go": "//go:build kannatest\n\npackage a\n\nimport _ \"example.com/m/b\"\n",
		"b/b.go":      "//go:build kannatest\n\npackage b\n\n//go:generate go tool kanna-fake -out b_gen.go\n",
	})

	got := kanna(t, dir, "gen", "--plan", "./a")
	if got.code != 0 {
		t.Fatalf("%+v", got)
	}
	contains(t, got.stdout, "1. b/b.go:5", "2. a/a.go:3", "reads b, which kanna-fake writes into")

	got = kanna(t, dir, "gen", "--check", "./b")
	if got.code != 1 {
		t.Fatalf("%+v", got)
	}
	contains(t, got.stdout, "b/b.go:5", "out of date")
}
