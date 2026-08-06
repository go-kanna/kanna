package di_test

import (
	"testing"

	"github.com/go-kanna/kanna/internal/gen/di"
)

func TestProviderRef_HasRef(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want bool
	}{
		{name: "empty", raw: "", want: false},
		{name: "func only", raw: "NewDB", want: true},
		{name: "qualified", raw: "config.NewWriterDB", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := di.ProviderRef{Raw: tt.raw}
			if got := r.HasRef(); got != tt.want {
				t.Errorf("HasRef() = %v, want %v", got, tt.want)
			}
		})
	}
}
