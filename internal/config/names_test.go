package config

import "testing"

func TestNamespaceTailscaleTagEncodesDomainNamespace(t *testing.T) {
	if got := NamespaceTailscaleTag("example.com"); got != "tag:serverpro-example-x2e-com" {
		t.Fatalf("NamespaceTailscaleTag() = %q", got)
	}
}

func TestNamespaceTailscaleTagAvoidsSlugCollisions(t *testing.T) {
	namespaces := []string{"foo.bar", "foo-bar", "foo_bar", "foo-x2e-bar"}
	seen := map[string]string{}
	for _, namespace := range namespaces {
		tag := NamespaceTailscaleTag(namespace)
		if other, ok := seen[tag]; ok {
			t.Fatalf("namespaces %q and %q produced same tag %q", other, namespace, tag)
		}
		seen[tag] = namespace
	}
}

func TestServerResourceNameEncodesProviderUnsafeIDs(t *testing.T) {
	if got := ServerResourceName("example.com", "api_blue"); got != "example-x2e-com-api-x5f-blue" {
		t.Fatalf("ServerResourceName() = %q", got)
	}
}
