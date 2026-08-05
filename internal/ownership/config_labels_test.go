package ownership

import "testing"

// WHY: ConfigLabels stamps the local-config ownership marker set. It must always
// carry managed-by and drop the retired flat project/server keys so local config
// never re-introduces the ambiguous pre-namespace layout.

func TestConfigLabelsStampCanonicalOwnershipAndDropRetiredKeys(t *testing.T) {
	labels := ConfigLabels("prod", "web", map[string]string{"project": "old", "server": "old", "team": "ops"})
	if labels[ManagedByKey] != ManagedByValue {
		t.Fatalf("missing managed-by marker: %#v", labels)
	}
	if labels[NamespaceKey] != "prod" || labels[ServerKey] != "web" {
		t.Fatalf("namespace/server markers wrong: %#v", labels)
	}
	if labels["team"] != "ops" {
		t.Fatalf("extra label dropped: %#v", labels)
	}
	if _, ok := labels["project"]; ok {
		t.Fatalf("retired project key leaked: %#v", labels)
	}
	if _, ok := labels["server"]; ok {
		t.Fatalf("retired flat server key leaked: %#v", labels)
	}
}

func TestConfigLabelsOmitEmptyNamespaceAndServer(t *testing.T) {
	labels := ConfigLabels("", "", nil)
	if labels[ManagedByKey] != ManagedByValue {
		t.Fatalf("managed-by must always be present: %#v", labels)
	}
	if _, ok := labels[NamespaceKey]; ok {
		t.Fatalf("empty namespace should not be stamped: %#v", labels)
	}
	if _, ok := labels[ServerKey]; ok {
		t.Fatalf("empty server should not be stamped: %#v", labels)
	}
}
