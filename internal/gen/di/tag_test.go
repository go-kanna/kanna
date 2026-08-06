package di_test

import (
	"testing"

	"github.com/go-kanna/kanna/internal/gen/di"
)

func TestParseTag(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		in      string
		want    di.ParsedTag
		wantErr string
	}{
		{name: "marker", in: "", want: di.ParsedTag{Kind: di.TagMarker}},
		{name: "marker with whitespace", in: "  ", want: di.ParsedTag{Kind: di.TagMarker}},
		{name: "with qualified", in: "with=foo.NewBar", want: di.ParsedTag{Kind: di.TagWith, With: "foo.NewBar"}},
		{name: "with bare func", in: "with=NewBar", want: di.ParsedTag{Kind: di.TagWith, With: "NewBar"}},
		{name: "with empty value", in: "with=", wantErr: `di:"with=..." requires a provider reference`},
		{name: "with no equals", in: "with", wantErr: `di:"with=..." requires a provider reference`},
		{name: "arg", in: "arg", want: di.ParsedTag{Kind: di.TagArg}},
		{name: "arg with name", in: "arg=primary", want: di.ParsedTag{Kind: di.TagArg, ArgName: "primary"}},
		{name: "arg empty name", in: "arg=", wantErr: `di:"arg=..." requires a name`},
		{name: "arg not an identifier", in: "arg=1st", wantErr: `di:"arg=1st" is not a valid Go identifier`},
		{name: "arg with a dot", in: "arg=pkg.Name", wantErr: `di:"arg=pkg.Name" is not a valid Go identifier`},
		{name: "returns", in: "returns", want: di.ParsedTag{Kind: di.TagReturns}},
		{name: "returns with value", in: "returns=foo", wantErr: `di:"returns" does not take a value`},
		{name: "embed", in: "embed", want: di.ParsedTag{Kind: di.TagEmbed}},
		{name: "embed with value", in: "embed=foo", wantErr: `di:"embed" does not take a value`},
		{name: "unknown bare", in: "xyz", wantErr: `unknown di tag form "xyz"`},
		{name: "unknown kv", in: "xyz=1", wantErr: `unknown di tag form "xyz=1"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := di.ParseTag(tt.in)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("ParseTag(%q) returned nil error, want %q", tt.in, tt.wantErr)
				}
				if err.Error() != tt.wantErr {
					t.Errorf("ParseTag(%q) error = %q, want %q", tt.in, err.Error(), tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Fatalf("ParseTag(%q) returned unexpected error: %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("ParseTag(%q) = %+v, want %+v", tt.in, got, tt.want)
			}
		})
	}
}
