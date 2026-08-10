package i18n_test

import (
	"testing"

	"github.com/go-kanna/kanna/i18n"
)

// Construction mistakes panic rather than erroring: the catalogs come from
// generated code whose inputs were validated at generation time, so reaching
// either branch means a hand-assembled bundle got it wrong.

func TestNewBundle_duplicateLanguagePanics(t *testing.T) {
	t.Parallel()

	defer func() {
		if recover() == nil {
			t.Error("NewBundle() with a duplicated language did not panic")
		}
	}()
	i18n.NewBundle("en",
		i18n.Catalog{Lang: "en"},
		i18n.Catalog{Lang: "en"},
	)
}

func TestNewBundle_missingDefaultCatalogPanics(t *testing.T) {
	t.Parallel()

	defer func() {
		if recover() == nil {
			t.Error("NewBundle() without a catalog for the default locale did not panic")
		}
	}()
	i18n.NewBundle("en", i18n.Catalog{Lang: "ja"})
}

// A bundle with no catalogs at all stays legal: it is the shape a package with
// no locale files generates, and its Localizers render every message as its key.
func TestNewBundle_noCatalogs(t *testing.T) {
	t.Parallel()

	i18n.NewBundle("en")
}

func TestNewBundle_invalidTagPanics(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		make func()
	}{
		{name: "default language", make: func() { i18n.NewBundle("not a tag") }},
		{name: "catalog language", make: func() { i18n.NewBundle("en", i18n.Catalog{Lang: "not a tag"}) }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			defer func() {
				if recover() == nil {
					t.Error("NewBundle() with an invalid tag did not panic")
				}
			}()
			tt.make()
		})
	}
}
