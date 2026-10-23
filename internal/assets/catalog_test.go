package assets

import (
	"errors"
	"strings"
	"testing"
)

func TestCatalogKindFromPath(t *testing.T) {
	for path, want := range map[string]CatalogKind{
		"manufacturers": CatalogManufacturer,
		"models":        CatalogModel,
		"locations":     CatalogLocation,
	} {
		got, ok := CatalogKindFromPath(path)
		if !ok || got != want {
			t.Fatalf("CatalogKindFromPath(%q) = %q, %v; want %q, true", path, got, ok, want)
		}
	}
	if _, ok := CatalogKindFromPath("users"); ok {
		t.Fatal("unknown catalog path should be rejected")
	}
}

func TestNormalizeCatalogInput(t *testing.T) {
	value, manufacturer, err := normalizeCatalogInput(CatalogModel, "  Latitude 7440 ", " Dell ")
	if err != nil || value != "Latitude 7440" || manufacturer != "Dell" {
		t.Fatalf("normalized model = %q, %q, %v", value, manufacturer, err)
	}
	for _, test := range []struct {
		kind         CatalogKind
		value        string
		manufacturer string
	}{
		{CatalogManufacturer, "", ""},
		{CatalogLocation, "Warszawa", "Dell"},
		{CatalogModel, "Laptop", strings.Repeat("x", 161)},
		{CatalogKind("UNKNOWN"), "Laptop", ""},
	} {
		if _, _, err := normalizeCatalogInput(test.kind, test.value, test.manufacturer); !errors.Is(err, ErrInvalid) {
			t.Fatalf("normalizeCatalogInput(%q, %q, ...) error = %v; want ErrInvalid", test.kind, test.value, err)
		}
	}
}
