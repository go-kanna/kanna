package orm_test

import (
	"testing"

	"github.com/go-kanna/kanna/internal/gen/orm"
)

func TestCamelToSnake(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		{"ID", "id"},
		{"Name", "name"},
		{"CreatedAt", "created_at"},
		{"UserID", "user_id"},
		{"HTTPServer", "http_server"},
		{"userProfile", "user_profile"},
		{"S3Object", "s3_object"},
		{"QRImageID", "qr_image_id"},
		{"EC2Instance", "ec2_instance"},
		{"A", "a"},
		{"", ""},
		// Mixed-case acronyms infer mechanically; a model wanting
		// "oauth_token" writes it in the tag.
		{"OAuth", "o_auth"},
		{"OAuthToken", "o_auth_token"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()

			got := orm.CamelToSnake(tt.input)
			if got != tt.want {
				t.Errorf("CamelToSnake(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

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
