package relation_test

import (
	"testing"

	"github.com/go-kanna/kanna/internal/relation"
)

func TestSnakeCase(t *testing.T) {
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

			got := relation.SnakeCase(tt.input)
			if got != tt.want {
				t.Errorf("SnakeCase(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
