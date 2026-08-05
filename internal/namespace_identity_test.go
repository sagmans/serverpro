package internal_test

import (
	"reflect"
	"testing"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/credentials"
	"github.com/sagmans/serverpro/internal/state"
)

func TestDomainStructsExposeOnlyCanonicalNamespaceIdentity(t *testing.T) {
	for name, typ := range map[string]reflect.Type{
		"config.Config":       reflect.TypeOf(config.Config{}),
		"credentials.Set":     reflect.TypeOf(credentials.Set{}),
		"state.State":         reflect.TypeOf(state.State{}),
		"state.RegistryEntry": reflect.TypeOf(state.RegistryEntry{}),
	} {
		if _, ok := typ.FieldByName("Project"); ok {
			t.Errorf("%s retains writable Project field", name)
		}
		if _, ok := typ.FieldByName("Namespace"); !ok {
			t.Errorf("%s missing canonical Namespace field", name)
		}
	}
	registry := reflect.TypeOf(state.Registry{})
	if _, ok := registry.FieldByName("Projects"); ok {
		t.Error("state.Registry retains Projects internals")
	}
	if _, ok := registry.FieldByName("Namespaces"); !ok {
		t.Error("state.Registry missing Namespaces internals")
	}
}
