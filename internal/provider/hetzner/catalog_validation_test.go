package hetzner

import (
	"strings"
	"testing"
)

func TestValidateSelectionRejectsLocationAndArchitectureMismatch(t *testing.T) {
	catalog := Catalog{
		Locations:   []Location{{Name: "fsn1"}},
		ServerTypes: []ServerType{{Name: "cax11", Architecture: "arm", Locations: []ServerTypeLocation{{Name: "hel1"}}}},
		Images:      []Image{{Name: "ubuntu-24.04", Architecture: "x86", Status: "available"}},
	}
	if err := catalog.ValidateSelection("fsn1", "cax11", "ubuntu-24.04"); err == nil || !strings.Contains(err.Error(), "not supported in location") {
		t.Fatalf("expected location mismatch, got %v", err)
	}
	catalog.ServerTypes[0].Locations = []ServerTypeLocation{{Name: "fsn1"}}
	if err := catalog.ValidateSelection("fsn1", "cax11", "ubuntu-24.04"); err == nil || !strings.Contains(err.Error(), "architecture") {
		t.Fatalf("expected arch mismatch, got %v", err)
	}
}

func TestValidateSelectionRejectsLocationDeprecatedServerType(t *testing.T) {
	catalog := Catalog{
		Locations: []Location{{Name: "fsn1"}},
		ServerTypes: []ServerType{{
			Name:         "cax11",
			Architecture: "arm",
			Locations:    []ServerTypeLocation{{Name: "fsn1", Deprecation: map[string]any{"announced": "2026-01-01T00:00:00Z"}}},
		}},
		Images: []Image{{Name: "ubuntu-24.04", Architecture: "arm", Status: "available"}},
	}
	if err := catalog.ValidateSelection("fsn1", "cax11", "ubuntu-24.04"); err == nil || !strings.Contains(err.Error(), "deprecated in location") {
		t.Fatalf("expected per-location deprecation, got %v", err)
	}
}

func TestValidateSelectionAcceptsAnyCompatibleDuplicateImageName(t *testing.T) {
	catalog := Catalog{
		Locations:   []Location{{Name: "fsn1"}},
		ServerTypes: []ServerType{{Name: "cax11", Architecture: "arm", Locations: []ServerTypeLocation{{Name: "fsn1"}}}},
		Images: []Image{
			{Name: "ubuntu-24.04", Architecture: "x86", Status: "available"},
			{Name: "ubuntu-24.04", Architecture: "arm", Status: "available"},
		},
	}
	if err := catalog.ValidateSelection("fsn1", "cax11", "ubuntu-24.04"); err != nil {
		t.Fatal(err)
	}
}

func TestValidateSelectionRejectsMissingLocationAndServerType(t *testing.T) {
	catalog := validCatalog()
	if err := catalog.ValidateSelection("hel1", "cax11", "ubuntu-24.04"); err == nil || !strings.Contains(err.Error(), "location \"hel1\" not found") {
		t.Fatalf("expected missing location, got %v", err)
	}
	if err := catalog.ValidateSelection("fsn1", "cx11", "ubuntu-24.04"); err == nil || !strings.Contains(err.Error(), "server type \"cx11\" not found") {
		t.Fatalf("expected missing server type, got %v", err)
	}
}

func TestValidateSelectionRejectsGloballyDeprecatedServerType(t *testing.T) {
	catalog := validCatalog()
	catalog.ServerTypes[0].Deprecation = map[string]any{"announced": "2026-01-01T00:00:00Z"}
	if err := catalog.ValidateSelection("fsn1", "cax11", "ubuntu-24.04"); err == nil || !strings.Contains(err.Error(), "server type \"cax11\" is deprecated") {
		t.Fatalf("expected server type deprecation, got %v", err)
	}
}

func TestValidateSelectionRejectsUnavailableDeprecatedAndMissingImages(t *testing.T) {
	cases := []struct {
		name    string
		images  []Image
		wantErr string
	}{
		{name: "missing", images: nil, wantErr: "not found"},
		{name: "unavailable", images: []Image{{Name: "ubuntu-24.04", Architecture: "arm", Status: "deprecated"}}, wantErr: "not available"},
		{name: "deprecated", images: []Image{{Name: "ubuntu-24.04", Architecture: "arm", Status: "available", Deprecated: map[string]any{"deprecated": true}}}, wantErr: "is deprecated"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			catalog := validCatalog()
			catalog.Images = tt.images
			if err := catalog.ValidateSelection("fsn1", "cax11", "ubuntu-24.04"); err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected %q, got %v", tt.wantErr, err)
			}
		})
	}
}

func validCatalog() Catalog {
	return Catalog{
		Locations:   []Location{{Name: "fsn1"}},
		ServerTypes: []ServerType{{Name: "cax11", Architecture: "arm", Locations: []ServerTypeLocation{{Name: "fsn1"}}}},
		Images:      []Image{{Name: "ubuntu-24.04", Architecture: "arm", Status: "available"}},
	}
}
