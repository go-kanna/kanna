package relation_test

import (
	"slices"
	"testing"

	"github.com/go-kanna/kanna/internal/relation"
)

func TestPickPrimaryKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		fields []relation.PKCandidate
		want   []int
	}{
		{
			name:   "explicit wins over ID",
			fields: []relation.PKCandidate{{Name: "ID"}, {Name: "UID", Explicit: true}},
			want:   []int{1},
		},
		{
			name:   "ID is the fallback",
			fields: []relation.PKCandidate{{Name: "Name"}, {Name: "ID"}},
			want:   []int{1},
		},
		{
			name:   "nothing qualifies",
			fields: []relation.PKCandidate{{Name: "Name"}, {Name: "Email"}},
			want:   nil,
		},
		{
			name: "several explicit keys are returned, not picked from",
			fields: []relation.PKCandidate{
				{Name: "A", Explicit: true},
				{Name: "B"},
				{Name: "C", Explicit: true},
			},
			want: []int{0, 2},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := relation.PickPrimaryKey(tt.fields)
			if !slices.Equal(got, tt.want) {
				t.Errorf("PickPrimaryKey() = %v, want %v", got, tt.want)
			}
		})
	}
}
