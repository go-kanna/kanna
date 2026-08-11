package orm_test

import (
	"testing"

	"github.com/go-kanna/kanna/orm"
)

type plain struct{}

type valueNamer struct{}

func (valueNamer) TableName() string { return "custom_values" }

type ptrNamer struct{}

func (*ptrNamer) TableName() string { return "custom_ptrs" }

func TestResolveTableName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		resolve  func() string
		expected string
	}{
		{
			name:     "fallback when TableNamer not implemented",
			resolve:  func() string { return orm.ResolveTableName[plain]("fallback") },
			expected: "fallback",
		},
		{
			name:     "value receiver",
			resolve:  func() string { return orm.ResolveTableName[valueNamer]("fallback") },
			expected: "custom_values",
		},
		{
			name:     "pointer receiver",
			resolve:  func() string { return orm.ResolveTableName[ptrNamer]("fallback") },
			expected: "custom_ptrs",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.resolve(); got != tt.expected {
				t.Errorf("got %q, want %q", got, tt.expected)
			}
		})
	}
}
