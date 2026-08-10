package i18n_test

import (
	"testing"

	i18n "github.com/go-kanna/kanna/internal/gen/i18n"
)

func TestFuncName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		key  string
		want string
	}{
		{key: "greeting", want: "Greeting"},
		{key: "items_count", want: "ItemsCount"},
		{key: "user.not_found", want: "UserNotFound"},
		{key: "a.b_c", want: "ABC"},
		{key: "step_2", want: "Step2"},
	}
	for _, tt := range tests {
		if got := i18n.FuncName(tt.key); got != tt.want {
			t.Errorf("FuncName(%q) = %q, want %q", tt.key, got, tt.want)
		}
	}
}

func TestParamName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		want string
	}{
		{name: "name", want: "name"},
		{name: "user_name", want: "userName"},
		{name: "a_b_c", want: "aBC"},
		{name: "_x", want: "x"},
	}
	for _, tt := range tests {
		got, err := i18n.ParamName(tt.name)
		if err != nil {
			t.Errorf("ParamName(%q) returned error: %v", tt.name, err)
			continue
		}
		if got != tt.want {
			t.Errorf("ParamName(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}

	for _, name := range []string{"range", "type", "func", "_", "__", "_2x"} {
		if _, err := i18n.ParamName(name); err == nil {
			t.Errorf("ParamName(%q) returned nil error", name)
		}
	}
}
