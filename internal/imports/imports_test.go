package imports_test

import (
	"go/types"
	"reflect"
	"testing"

	"github.com/go-kanna/kanna/internal/imports"
)

func TestTrackerNames(t *testing.T) {
	t.Parallel()

	suffixReserved := func(taken string) func(string) string {
		return func(base string) string {
			if base == taken {
				return base + "pkg"
			}
			return base
		}
	}

	tests := []struct {
		name    string
		self    string
		rename  func(string) string
		reserve []string
		pkgs    [][2]string // path, name
		want    []string
	}{
		{
			name: "distinct names are left alone",
			self: "example.com/output",
			pkgs: [][2]string{{"example.com/model", "model"}, {"example.com/wire", "wire"}},
			want: []string{"model", "wire"},
		},
		{
			name: "a shared name is numbered",
			self: "example.com/output",
			pkgs: [][2]string{{"example.com/a/model", "model"}, {"example.com/b/model", "model"}},
			want: []string{"model", "model2"},
		},
		{
			name: "the same path keeps its name",
			self: "example.com/output",
			pkgs: [][2]string{{"example.com/a/model", "model"}, {"example.com/a/model", "model"}},
			want: []string{"model", "model"},
		},
		{
			name: "the file's own package is never qualified",
			self: "example.com/output",
			pkgs: [][2]string{{"example.com/output", "output"}},
			want: []string{""},
		},
		{
			// Reserve is for a generator that knows the exact names its body
			// declares: the import is numbered out of the way.
			name:    "a reserved name is numbered",
			self:    "example.com/output",
			reserve: []string{"log"},
			pkgs:    [][2]string{{"example.com/log", "log"}},
			want:    []string{"log2"},
		},
		{
			// rename is for a generator whose body names follow a pattern it
			// cannot enumerate: the base is rewritten instead.
			name:   "a renamed base takes a suffix",
			self:   "example.com/output",
			rename: suffixReserved("src"),
			pkgs:   [][2]string{{"example.com/src", "src"}},
			want:   []string{"srcpkg"},
		},
		{
			name:   "a renamed base still numbers on collision",
			self:   "example.com/output",
			rename: suffixReserved("src"),
			pkgs: [][2]string{
				{"example.com/a/src", "src"},
				{"example.com/b/src", "src"},
			},
			want: []string{"srcpkg", "srcpkg2"},
		},
		{
			name: "a package with no name falls back to its path",
			self: "example.com/output",
			pkgs: [][2]string{{"example.com/gen/v1", ""}},
			want: []string{"v1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tr := imports.New(tt.self, tt.rename)
			tr.Reserve(tt.reserve...)

			got := make([]string, 0, len(tt.pkgs))
			for _, p := range tt.pkgs {
				got = append(got, tr.Add(p[0], p[1]))
			}

			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("names = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTrackerQualifier(t *testing.T) {
	t.Parallel()

	tr := imports.New("example.com/output", nil)

	// The qualifier records as it goes, which is what lets a caller collect
	// imports simply by rendering types.
	if got := tr.Qualifier(types.NewPackage("example.com/model", "model")); got != "model" {
		t.Errorf("Qualifier() = %q, want %q", got, "model")
	}
	if got := tr.Qualifier(types.NewPackage("example.com/output", "output")); got != "" {
		t.Errorf("Qualifier(self) = %q, want empty", got)
	}
	if got := tr.Qualifier(nil); got != "" {
		t.Errorf("Qualifier(nil) = %q, want empty", got)
	}

	want := []imports.Entry{{Path: "example.com/model", Alias: "model"}}
	if got := tr.Entries(); !reflect.DeepEqual(got, want) {
		t.Errorf("Entries() = %+v, want %+v", got, want)
	}
}

func TestTrackerEntriesSortedByPath(t *testing.T) {
	t.Parallel()

	tr := imports.New("example.com/output", nil)
	for _, p := range []string{"example.com/z", "example.com/a", "example.com/m"} {
		tr.Add(p, "pkg")
	}

	want := []string{"example.com/a", "example.com/m", "example.com/z"}
	got := make([]string, 0, 3)
	for _, e := range tr.Entries() {
		got = append(got, e.Path)
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("paths = %v, want %v", got, want)
	}
}

func TestTrackerEntriesEmpty(t *testing.T) {
	t.Parallel()

	if got := imports.New("example.com/output", nil).Entries(); len(got) != 0 {
		t.Errorf("Entries() = %+v, want none", got)
	}
}
