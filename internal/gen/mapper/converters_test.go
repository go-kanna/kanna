package mapper_test

import (
	"fmt"
	"go/types"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/go-kanna/kanna/internal/gen/mapper"
	"github.com/go-kanna/kanna/internal/packages"
)

const fixturePrefix = "github.com/go-kanna/kanna/internal/gen/mapper/testdata/"

// fixtureDirs lists every testdata directory holding Go files.
//
// The "..." pattern does not expand under testdata — that is the whole point of
// the directory — so the packages are discovered here and named individually.
func fixtureDirs() ([]string, error) {
	var dirs []string

	err := filepath.WalkDir("testdata", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("walk %s: %w", path, err)
		}
		if !d.IsDir() {
			return nil
		}

		entries, err := os.ReadDir(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}

		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".go") {
				dirs = append(dirs, "./"+path)
				break
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan testdata: %w", err)
	}

	return dirs, nil
}

var loadFixtures = sync.OnceValues(func() (map[string]*packages.Package, error) {
	dirs, err := fixtureDirs()
	if err != nil {
		return nil, err
	}

	// Converter extraction reads what a call refers to, so the loader has to
	// keep the type-checking results.
	res, err := packages.Load(dirs, packages.Config{TypesInfo: true})
	if err != nil {
		return nil, fmt.Errorf("load fixture packages: %w", err)
	}

	byName := make(map[string]*packages.Package, len(res.Packages))
	for _, pkg := range res.Packages {
		if len(pkg.Errors) > 0 {
			return nil, fmt.Errorf("fixture %s has load errors: %v", pkg.PkgPath, pkg.Errors)
		}
		byName[strings.TrimPrefix(pkg.PkgPath, fixturePrefix)] = pkg
	}
	return byName, nil
})

func fixture(t *testing.T, name string) *packages.Package {
	t.Helper()

	pkgs, err := loadFixtures()
	if err != nil {
		t.Fatalf("load fixtures: %v", err)
	}
	pkg, ok := pkgs[name]
	if !ok {
		t.Fatalf("fixture %q not loaded", name)
	}
	return pkg
}

func importedType(t *testing.T, pkg *packages.Package, path, name string) types.Type {
	t.Helper()

	for _, imp := range pkg.Types.Imports() {
		if imp.Path() == path {
			return imp.Scope().Lookup(name).Type()
		}
	}
	t.Fatalf("package %s does not import %s", pkg.PkgPath, path)
	return nil
}

func TestExtractConverters(t *testing.T) {
	t.Parallel()

	pkg := fixture(t, "converters/ok")
	table, err := mapper.ExtractConverters([]*packages.Package{pkg}, "example.com/output")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := table.Len(); got != 5 {
		t.Errorf("got %d converters, want 5", got)
	}

	scope := pkg.Types.Scope()
	userID := scope.Lookup("UserID").Type()
	timestamp := scope.Lookup("Timestamp").Type()
	timeT := importedType(t, pkg, "time", "Time")
	stringT := types.Typ[types.String]
	intT := types.Typ[types.Int]

	tests := []struct {
		name string
		src  types.Type
		dst  types.Type
		want mapper.ConverterInfo
	}{
		{
			name: "ident argument with inferred type args",
			src:  userID,
			dst:  stringT,
			want: mapper.ConverterInfo{Func: "FormatUserID", PkgPath: pkg.PkgPath},
		},
		{
			name: "explicit type args",
			src:  timestamp,
			dst:  timeT,
			want: mapper.ConverterInfo{Func: "ToTime", PkgPath: pkg.PkgPath},
		},
		{
			name: "error-returning converter",
			src:  stringT,
			dst:  userID,
			want: mapper.ConverterInfo{Func: "ParseUserID", PkgPath: pkg.PkgPath, HasErr: true},
		},
		{
			name: "function from another package",
			src:  stringT,
			dst:  intT,
			want: mapper.ConverterInfo{Func: "Atoi", PkgPath: "strconv", HasErr: true},
		},
		{
			name: "registration outside init through aliased import",
			src:  timeT,
			dst:  timestamp,
			want: mapper.ConverterInfo{Func: "Truncate", PkgPath: pkg.PkgPath},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := table.LookupInfo(tt.src, tt.dst)
			if !ok {
				t.Fatalf("converter from %s to %s not found", tt.src, tt.dst)
			}
			if got != tt.want {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestExtractConvertersLookupMiss(t *testing.T) {
	t.Parallel()

	pkg := fixture(t, "converters/ok")
	table, err := mapper.ExtractConverters([]*packages.Package{pkg}, "example.com/output")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := table.LookupInfo(types.Typ[types.Int], types.Typ[types.String]); ok {
		t.Error("expected lookup miss for unregistered pair")
	}
}

func TestExtractConvertersErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		fixture string
		wantErr string
	}{
		{"converters/closure", "function literal"},
		{"converters/dup", "already registered"},
		{"converters/funcvar", "must be a named function"},
		{"converters/genericfn", "generic converter functions are not supported"},
		{"converters/method", "must not be a method"},
		{"converters/unexported", "must be exported"},
	}
	for _, tt := range tests {
		t.Run(tt.fixture, func(t *testing.T) {
			t.Parallel()
			pkg := fixture(t, tt.fixture)
			_, err := mapper.ExtractConverters([]*packages.Package{pkg}, "example.com/output")
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q does not contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestExtractConvertersUnexportedInOutputPackage(t *testing.T) {
	t.Parallel()

	pkg := fixture(t, "converters/unexported")
	table, err := mapper.ExtractConverters([]*packages.Package{pkg}, pkg.PkgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := table.Len(); got != 1 {
		t.Errorf("got %d converters, want 1", got)
	}
}
