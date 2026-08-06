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
