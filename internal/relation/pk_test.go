package relation_test

import (
	"slices"
	"testing"

	"github.com/go-kanna/kanna/internal/relation"
)

func TestPickPrimaryKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		fields    []relation.PKCandidate
		wantIdx   int
		wantDupes []int
	}{
		{
			name:    "explicit wins over ID",
			fields:  []relation.PKCandidate{{Name: "ID"}, {Name: "UID", Explicit: true}},
			wantIdx: 1,
		},
		{
			name:    "ID is the fallback",
			fields:  []relation.PKCandidate{{Name: "Name"}, {Name: "ID"}},
			wantIdx: 1,
		},
		{
			name:    "nothing qualifies",
			fields:  []relation.PKCandidate{{Name: "Name"}, {Name: "Email"}},
			wantIdx: -1,
		},
		{
			name: "several explicit keys are returned, not picked from",
			fields: []relation.PKCandidate{
				{Name: "A", Explicit: true},
				{Name: "B"},
				{Name: "C", Explicit: true},
			},
			wantIdx:   -1,
			wantDupes: []int{0, 2},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			idx, dupes := relation.PickPrimaryKey(tt.fields)
			if idx != tt.wantIdx {
				t.Errorf("idx = %d, want %d", idx, tt.wantIdx)
			}
			if !slices.Equal(dupes, tt.wantDupes) {
				t.Errorf("dupes = %v, want %v", dupes, tt.wantDupes)
			}
		})
	}
}
