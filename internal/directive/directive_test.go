package directive_test

import (
	"go/token"
	"reflect"
	"testing"

	"github.com/go-kanna/kanna/internal/diag"
	"github.com/go-kanna/kanna/internal/directive"
)

func TestFind(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		lines        []string
		key          string
		want         directive.Directive
		wantErrors   []string
		wantWarnings []string
	}{
		{
			name: "no lines",
			key:  "ignore",
		},
		{
			name:  "unrelated comment",
			lines: []string{"// Foo stands in for a real record."},
			key:   "ignore",
		},
		{
			name:  "marker only",
			lines: []string{"//kanna:ignore"},
			key:   "ignore",
			want:  directive.Directive{Found: true},
		},
		{
			name:  "arguments",
			lines: []string{"//kanna:container name=NewFoo must"},
			key:   "container",
			want:  directive.Directive{Found: true, Args: "name=NewFoo must"},
		},
		{
			name:  "trailing space",
			lines: []string{"//kanna:ignore   "},
			key:   "ignore",
			want:  directive.Directive{Found: true},
		},
		{
			name:  "tab before the arguments",
			lines: []string{"//kanna:container\tname=NewFoo"},
			key:   "container",
			want:  directive.Directive{Found: true, Args: "name=NewFoo"},
		},
		{
			name:  "found among other comment lines",
			lines: []string{"// User is a person.", "//kanna:ignore", "// Not generated."},
			key:   "ignore",
			want:  directive.Directive{Found: true},
		},
		{
			name:  "another generator's key",
			lines: []string{"//kanna:container"},
			key:   "ignore",
		},
		{
			name:  "longer key sharing the prefix",
			lines: []string{"//kanna:ignoreXYZ"},
			key:   "ignore",
		},
		{
			name:  "block comment",
			lines: []string{"/* kanna:ignore */"},
			key:   "ignore",
		},
		{
			name:  "prose mentioning the directive",
			lines: []string{"// Mark it with kanna:ignore to skip it."},
			key:   "ignore",
		},
		{
			name:  "space after the comment marker",
			lines: []string{"// kanna:ignore"},
			key:   "ignore",
			wantWarnings: []string{
				`"// kanna:ignore" is not recognized as a directive; write it as "//kanna:ignore" with no space`,
			},
		},
		{
			// The suggestion has to carry the arguments, or following it costs
			// the author what they wrote.
			name:  "several spaces after the comment marker",
			lines: []string{"//   kanna:container name=A"},
			key:   "container",
			wantWarnings: []string{
				`"//   kanna:container name=A" is not recognized as a directive; ` +
					`write it as "//kanna:container name=A" with no space`,
			},
		},
		{
			name:  "spaced line does not satisfy a later duplicate check",
			lines: []string{"// kanna:ignore", "//kanna:ignore"},
			key:   "ignore",
			want:  directive.Directive{Found: true},
			wantWarnings: []string{
				`"// kanna:ignore" is not recognized as a directive; write it as "//kanna:ignore" with no space`,
			},
		},
		{
			name:       "duplicate keeps the first",
			lines:      []string{"//kanna:container name=A", "//kanna:container name=B"},
			key:        "container",
			want:       directive.Directive{Found: true, Args: "name=A"},
			wantErrors: []string{"duplicate //kanna:container directive"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, msgs := directive.Find(tt.lines, tt.key)

			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Find() = %+v, want %+v", got, tt.want)
			}
			if !reflect.DeepEqual(msgs.Errors, tt.wantErrors) {
				t.Errorf("errors = %q, want %q", msgs.Errors, tt.wantErrors)
			}
			if !reflect.DeepEqual(msgs.Warnings, tt.wantWarnings) {
				t.Errorf("warnings = %q, want %q", msgs.Warnings, tt.wantWarnings)
			}
		})
	}
}

func TestMessagesDiags(t *testing.T) {
	t.Parallel()

	pos := token.Position{Filename: "model.go", Line: 7, Column: 1}
	msgs := directive.Messages{
		Errors:   []string{"boom"},
		Warnings: []string{"careful"},
	}

	got := msgs.Diags(pos)

	want := []diag.Diag{
		{Severity: diag.SeverityError, Pos: pos, Message: "boom"},
		{Severity: diag.SeverityWarning, Pos: pos, Message: "careful"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Diags() = %+v, want %+v", got, want)
	}
}

func TestMessagesDiagsEmpty(t *testing.T) {
	t.Parallel()

	if got := (directive.Messages{}).Diags(token.Position{}); len(got) != 0 {
		t.Errorf("Diags() = %+v, want none", got)
	}
}
