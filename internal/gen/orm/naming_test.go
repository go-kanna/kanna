package orm_test

import (
	"testing"

	"github.com/go-kanna/kanna/internal/gen/orm"
)

func TestTableName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		{"User", "users"},
		{"UserProfile", "user_profiles"},
		{"Person", "people"},
		{"Status", "statuses"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()

			got := orm.TableName(tt.input)
			if got != tt.want {
				t.Errorf("TableName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestFactoryName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		{"User", "Users"},
		{"UserProfile", "UserProfiles"},
		{"Person", "People"},
		{"OAuthClient", "OAuthClients"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()

			got := orm.FactoryName(tt.input)
			if got != tt.want {
				t.Errorf("FactoryName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
