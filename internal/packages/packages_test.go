package packages_test

import (
	"strings"
	"testing"

	"github.com/go-kanna/kanna/internal/packages"
)

func TestLoad_NoPatterns(t *testing.T) {
	t.Parallel()

	_, err := packages.Load(nil, packages.Config{})
	if err == nil {
		t.Fatal("Load(nil) returned nil error")
	}
	if !strings.Contains(err.Error(), "no package patterns") {
		t.Errorf("error = %q, want substring %q", err, "no package patterns")
	}
}

// TypesInfo decides whether the loader retains what expressions refer to, which
// is the difference between a generator that can read a call and one that
// cannot.
func TestLoad_TypesInfo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  packages.Config
		want bool
	}{
		{name: "off by default", cfg: packages.Config{}, want: false},
		{name: "opt in", cfg: packages.Config{TypesInfo: true}, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			res, err := packages.Load([]string{"github.com/go-kanna/kanna/internal/packages"}, tt.cfg)
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if len(res.Packages) != 1 {
				t.Fatalf("packages = %d, want 1", len(res.Packages))
			}

			info := res.Packages[0].TypesInfo
			if got := info != nil && len(info.Uses) > 0; got != tt.want {
				t.Errorf("TypesInfo populated = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestJoinTags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		tags []string
		want string
	}{
		{name: "nil", tags: nil, want: ""},
		{name: "empty slice", tags: []string{}, want: ""},
		{name: "single", tags: []string{"integration"}, want: "integration"},
		{name: "multiple", tags: []string{"integration", "e2e"}, want: "integration e2e"},
		{name: "drops empty entries", tags: []string{"a", "", "b"}, want: "a b"},
		{name: "drops whitespace-only entries", tags: []string{"a", "   ", "b"}, want: "a b"},
		{name: "trims surrounding whitespace", tags: []string{"  a  ", "\tb\n"}, want: "a b"},
		{name: "all empty", tags: []string{"", "  "}, want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := packages.JoinTags(tt.tags); got != tt.want {
				t.Errorf("JoinTags(%q) = %q, want %q", tt.tags, got, tt.want)
			}
		})
	}
}
