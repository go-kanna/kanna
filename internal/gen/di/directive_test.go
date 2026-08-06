package di_test

import (
	"reflect"
	"slices"
	"testing"

	"github.com/go-kanna/kanna/internal/gen/di"
)

func TestParseDirective(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		lines    []string
		wantPD   di.ParsedDirective
		wantErrs []string
	}{
		{
			name: "no comments",
		},
		{
			name:  "no directive",
			lines: []string{"// this is just a doc"},
		},
		{
			name:   "marker only",
			lines:  []string{"//kanna:container"},
			wantPD: di.ParsedDirective{Found: true},
		},
		{
			name:   "marker with leading space",
			lines:  []string{"// kanna:container"},
			wantPD: di.ParsedDirective{Found: true},
		},
		{
			name:   "marker with trailing space",
			lines:  []string{"//kanna:container  "},
			wantPD: di.ParsedDirective{Found: true},
		},
		{
			name:   "name=",
			lines:  []string{"//kanna:container name=NewFoo"},
			wantPD: di.ParsedDirective{Found: true, Name: "NewFoo"},
		},
		{
			name:   "returns=",
			lines:  []string{"//kanna:container returns=greeter.Greeter"},
			wantPD: di.ParsedDirective{Found: true, ReturnsExpr: "greeter.Greeter"},
		},
		{
			name:   "must shorthand",
			lines:  []string{"//kanna:container must"},
			wantPD: di.ParsedDirective{Found: true, Must: di.MustOn},
		},
		{
			name:   "must=true",
			lines:  []string{"//kanna:container must=true"},
			wantPD: di.ParsedDirective{Found: true, Must: di.MustOn},
		},
		{
			name:   "must=false",
			lines:  []string{"//kanna:container must=false"},
			wantPD: di.ParsedDirective{Found: true, Must: di.MustOff},
		},
		{
			name:   "all three",
			lines:  []string{"//kanna:container name=NewFoo returns=foo.Foo must"},
			wantPD: di.ParsedDirective{Found: true, Name: "NewFoo", ReturnsExpr: "foo.Foo", Must: di.MustOn},
		},
		{
			name:     "duplicate directive",
			lines:    []string{"//kanna:container name=A", "//kanna:container name=B"},
			wantPD:   di.ParsedDirective{Found: true, Name: "A"},
			wantErrs: []string{"duplicate //kanna:container directive"},
		},
		{
			name:     "unknown key",
			lines:    []string{"//kanna:container xyz=1"},
			wantPD:   di.ParsedDirective{Found: true},
			wantErrs: []string{`unknown directive key "xyz"`},
		},
		{
			name:     "name without value",
			lines:    []string{"//kanna:container name="},
			wantPD:   di.ParsedDirective{Found: true},
			wantErrs: []string{"directive name= requires a value"},
		},
		{
			name:     "name is not an identifier",
			lines:    []string{"//kanna:container name=1st"},
			wantPD:   di.ParsedDirective{Found: true},
			wantErrs: []string{"directive name=1st is not a valid Go identifier"},
		},
		{
			name:     "name with a dot",
			lines:    []string{"//kanna:container name=pkg.New"},
			wantPD:   di.ParsedDirective{Found: true},
			wantErrs: []string{"directive name=pkg.New is not a valid Go identifier"},
		},
		{
			name:     "returns without =",
			lines:    []string{"//kanna:container returns"},
			wantPD:   di.ParsedDirective{Found: true},
			wantErrs: []string{"directive returns= requires a value"},
		},
		{
			name:     "must invalid value",
			lines:    []string{"//kanna:container must=panic"},
			wantPD:   di.ParsedDirective{Found: true},
			wantErrs: []string{`directive must= must be true or false, got "panic"`},
		},
		{
			name:     "must twice",
			lines:    []string{"//kanna:container must must=false"},
			wantPD:   di.ParsedDirective{Found: true, Must: di.MustOn},
			wantErrs: []string{"directive must specified more than once"},
		},
		{
			name:     "name twice",
			lines:    []string{"//kanna:container name=A name=B"},
			wantPD:   di.ParsedDirective{Found: true, Name: "A"},
			wantErrs: []string{"directive name= specified more than once"},
		},
		{
			name:     "returns twice",
			lines:    []string{"//kanna:container returns=a.A returns=b.B"},
			wantPD:   di.ParsedDirective{Found: true, ReturnsExpr: "a.A"},
			wantErrs: []string{"directive returns= specified more than once"},
		},
		{
			name:   "other directives are ignored",
			lines:  []string{"//go:generate something", "//kanna:ignore", "// kanna:container name=X"},
			wantPD: di.ParsedDirective{Found: true, Name: "X"},
		},
		{
			name:   "tab separator",
			lines:  []string{"//kanna:container\tname=Foo"},
			wantPD: di.ParsedDirective{Found: true, Name: "Foo"},
		},
		{
			name:  "tag without separator is rejected",
			lines: []string{"//kanna:containerXYZ"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotPD, gotErrs := di.ParseDirective(tt.lines)
			if !reflect.DeepEqual(gotPD, tt.wantPD) {
				t.Errorf("ParsedDirective = %+v, want %+v", gotPD, tt.wantPD)
			}
			if !slices.Equal(gotErrs, tt.wantErrs) {
				t.Errorf("errs = %v, want %v", gotErrs, tt.wantErrs)
			}
		})
	}
}
