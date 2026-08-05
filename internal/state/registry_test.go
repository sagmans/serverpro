package state

import "testing"

func TestNewRegistryInitializesDefaults(t *testing.T) {
	reg := NewRegistry()
	if reg.SchemaVersion != RegistrySchemaVersion {
		t.Fatalf("schema version = %d", reg.SchemaVersion)
	}
	if reg.Projects == nil {
		t.Fatal("namespaces map is nil")
	}
}
