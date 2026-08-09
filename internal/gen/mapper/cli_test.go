package mapper_test

import (
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/go-kanna/kanna/internal/gen/mapper"
)

func TestParse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want mapper.Config
	}{
		{
			name: "single pair",
			args: []string{"-types=model.Employee:*employeev1.Employee"},
			want: mapper.Config{
				Pairs: []mapper.TypePair{{
					Src: mapper.TypeRef{Pkg: "model", Name: "Employee"},
					Dst: mapper.TypeRef{Pkg: "employeev1", Name: "Employee", Pointer: true},
				}},
				Output:    ".",
				Direction: mapper.DirectionBoth,
			},
		},
		{
			name: "multiple pairs comma-separated and repeated",
			args: []string{"-types=model.A:pb.A,model.B:pb.B", "-types=model.C:pb.C"},
			want: mapper.Config{
				Pairs: []mapper.TypePair{
					{Src: mapper.TypeRef{Pkg: "model", Name: "A"}, Dst: mapper.TypeRef{Pkg: "pb", Name: "A"}},
					{Src: mapper.TypeRef{Pkg: "model", Name: "B"}, Dst: mapper.TypeRef{Pkg: "pb", Name: "B"}},
					{Src: mapper.TypeRef{Pkg: "model", Name: "C"}, Dst: mapper.TypeRef{Pkg: "pb", Name: "C"}},
				},
				Output:    ".",
				Direction: mapper.DirectionBoth,
			},
		},
		{
			name: "full import paths",
			args: []string{"-types=github.com/acme/app/internal/model.Employee:*github.com/acme/app/gen/employee/v1.Employee"},
			want: mapper.Config{
				Pairs: []mapper.TypePair{{
					Src: mapper.TypeRef{Pkg: "github.com/acme/app/internal/model", Name: "Employee"},
					Dst: mapper.TypeRef{Pkg: "github.com/acme/app/gen/employee/v1", Name: "Employee", Pointer: true},
				}},
				Output:    ".",
				Direction: mapper.DirectionBoth,
			},
		},
		{
			name: "import path with dotted element",
			args: []string{"-types=gopkg.in/yaml.v3.Node:model.Node"},
			want: mapper.Config{
				Pairs: []mapper.TypePair{{
					Src: mapper.TypeRef{Pkg: "gopkg.in/yaml.v3", Name: "Node"},
					Dst: mapper.TypeRef{Pkg: "model", Name: "Node"},
				}},
				Output:    ".",
				Direction: mapper.DirectionBoth,
			},
		},
		{
			name: "type in output package without selector",
			args: []string{"-types=Employee:pb.Employee"},
			want: mapper.Config{
				Pairs: []mapper.TypePair{{
					Src: mapper.TypeRef{Name: "Employee"},
					Dst: mapper.TypeRef{Pkg: "pb", Name: "Employee"},
				}},
				Output:    ".",
				Direction: mapper.DirectionBoth,
			},
		},
		{
			name: "all flags",
			args: []string{
				"-types=a.A:b.B",
				"-converter-pkg=./lib/converters,./lib/more",
				"-converter-pkg=github.com/acme/x",
				"-ignore=model.Employee.CreatedAt,Employee.ID",
				"-direction=to",
				"-package=handler",
				"-output=./gen",
				"-check",
			},
			want: mapper.Config{
				Pairs: []mapper.TypePair{
					{Src: mapper.TypeRef{Pkg: "a", Name: "A"}, Dst: mapper.TypeRef{Pkg: "b", Name: "B"}},
				},
				ConverterPkgs: []string{"./lib/converters", "./lib/more", "github.com/acme/x"},
				Ignores: []mapper.FieldRef{
					{Type: mapper.TypeRef{Pkg: "model", Name: "Employee"}, Field: "CreatedAt"},
					{Type: mapper.TypeRef{Name: "Employee"}, Field: "ID"},
				},
				Output:    "./gen",
				Direction: mapper.DirectionTo,
				Package:   "handler",
				Check:     true,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := mapper.Parse(tt.args, io.Discard)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %+v\nwant %+v", got, tt.want)
			}
		})
	}
}

func TestParseError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{"missing types", []string{}, "-types is required"},
		{"missing colon", []string{"-types=model.Employee"}, "want SRC:DST"},
		{"too many colons", []string{"-types=a.A:b.B:c.C"}, "want SRC:DST"},
		{"empty src", []string{"-types=:b.B"}, "want SRC:DST"},
		{"empty dst", []string{"-types=a.A:"}, "want SRC:DST"},
		{"invalid type name", []string{"-types=model.9x:b.B"}, "not a valid type name"},
		{"keyword as type name", []string{"-types=model.func:b.B"}, "not a valid type name"},
		{"keyword as package selector", []string{"-types=type.A:b.B"}, "not a valid package selector"},
		{"keyword as package flag", []string{"-types=a.A:b.B", "-package=func"}, "invalid -package"},
		{"bare pointer", []string{"-types=*:b.B"}, "not a valid type name"},
		{"empty package selector", []string{"-types=.Employee:b.B"}, "empty package selector"},
		{"invalid package selector", []string{"-types=mo-del.A:b.B"}, "not a valid package selector"},
		{"invalid direction", []string{"-types=a.A:b.B", "-direction=up"}, "invalid -direction"},
		{"ignore without field", []string{"-types=a.A:b.B", "-ignore=Employee"}, "want TYPE.FIELD"},
		{"ignore with pointer", []string{"-types=a.A:b.B", "-ignore=*model.Employee.ID"}, "pointer marker is not allowed"},
		{"invalid package flag", []string{"-types=a.A:b.B", "-package=9pkg"}, "invalid -package"},
		{"empty output", []string{"-types=a.A:b.B", "-output="}, "-output must not be empty"},
		{"unknown flag", []string{"-types=a.A:b.B", "-bogus"}, "parse arguments"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := mapper.Parse(tt.args, io.Discard)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q does not contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestTypeRefIsImportPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		pkg  string
		want bool
	}{
		{"", false},
		{"model", false},
		{"github.com/acme/app/internal/model", true},
		{"gopkg.in/yaml.v3", true},
	}
	for _, tt := range tests {
		ref := mapper.TypeRef{Pkg: tt.pkg, Name: "T"}
		if got := ref.IsImportPath(); got != tt.want {
			t.Errorf("IsImportPath(%q) = %v, want %v", tt.pkg, got, tt.want)
		}
	}
}
