package relation_test

import (
	"strings"
	"testing"

	"github.com/go-kanna/kanna/internal/relation"
)

func TestParseTagColumns(t *testing.T) {
	t.Parallel()

	tests := []struct {
		value string
		want  relation.ColumnTag
	}{
		{"email_address", relation.ColumnTag{Column: "email_address"}},
		{"", relation.ColumnTag{}},
		{"-", relation.ColumnTag{Skip: true}},
		{",primary_key", relation.ColumnTag{PrimaryKey: true}},
		{"uid,primary_key", relation.ColumnTag{Column: "uid", PrimaryKey: true}},
		{",created_at", relation.ColumnTag{CreatedAt: true}},
		{",updated_at", relation.ColumnTag{UpdatedAt: true}},
	}

	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			t.Parallel()

			col, rel, errs := relation.ParseTag(tt.value)
			if len(errs) != 0 {
				t.Fatalf("errs = %v", errs)
			}
			if rel != nil {
				t.Fatalf("parsed as relation: %+v", rel)
			}
			if *col != tt.want {
				t.Errorf("col = %+v, want %+v", *col, tt.want)
			}
		})
	}
}

func TestParseTagRelations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		value string
		want  relation.RelationTag
	}{
		{"has_many,foreign_key:user_id", relation.RelationTag{Kind: "has_many", ForeignKey: "user_id"}},
		{"has_one,foreign_key:user_id", relation.RelationTag{Kind: "has_one", ForeignKey: "user_id"}},
		{"belongs_to,foreign_key:user_id", relation.RelationTag{Kind: "belongs_to", ForeignKey: "user_id"}},
		{
			"many_to_many,join_table:user_tags,foreign_key:user_id,references:tag_id",
			relation.RelationTag{Kind: "many_to_many", ForeignKey: "user_id", JoinTable: "user_tags", References: "tag_id"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			t.Parallel()

			col, rel, errs := relation.ParseTag(tt.value)
			if len(errs) != 0 {
				t.Fatalf("errs = %v", errs)
			}
			if col != nil {
				t.Fatalf("parsed as column: %+v", col)
			}
			if *rel != tt.want {
				t.Errorf("rel = %+v, want %+v", *rel, tt.want)
			}
		})
	}
}

func TestParseTagErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		value   string
		wantErr string
	}{
		{"email,unique", `unknown option "unique"`},
		{"email,primary_keys", `unknown option "primary_keys"`},
		{"email,", "empty option"},
		{"-,primary_key", `"-" cannot be combined`},
		{"has_many", "requires foreign_key"},
		{"has_many,foreign_key:", "must take the form key:value"},
		{"has_many,foreign_key:a,foreign_key:b", `duplicate relation option "foreign_key"`},
		{"has_many,foreign_key:user_id,join_table:x", "join_table is only valid for many_to_many"},
		{"has_many,foreign_key:user_id,references:x", "references is only valid for many_to_many"},
		{"many_to_many,foreign_key:user_id", "requires join_table"},
		{"many_to_many,foreign_key:user_id,join_table:user_tags", "requires references"},
		{"has_many,fk:user_id", `unknown relation option "fk"`},
	}

	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			t.Parallel()

			_, _, errs := relation.ParseTag(tt.value)
			if len(errs) == 0 {
				t.Fatalf("expected error containing %q, got none", tt.wantErr)
			}
			joined := strings.Join(errs, "; ")
			if !strings.Contains(joined, tt.wantErr) {
				t.Errorf("errs = %q, want substring %q", joined, tt.wantErr)
			}
		})
	}
}
