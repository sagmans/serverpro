package ownership

import (
	"slices"
	"strings"
	"testing"
)

func TestProviderLabelsWritePortableOwnershipAndDropAmbiguousExtras(t *testing.T) {
	labels := ProviderLabels("prod", "web", map[string]string{"project": "old", "server": "old", "env.name": "prod.env"})
	if labels[ManagedByKey] != ManagedByValue || labels[ProviderTagNamespaceKey] != "prod" || labels[ProviderTagServerKey] != "web" || labels["env-name"] != "prod.env" {
		t.Fatalf("labels = %#v", labels)
	}
	for _, key := range []string{"project", "server", NamespaceKey, ServerKey} {
		if _, ok := labels[key]; ok {
			t.Fatalf("reserved label %q leaked: %#v", key, labels)
		}
	}
}

func TestOwnershipFromLabelsRequiresManagedMarkers(t *testing.T) {
	namespace, server, ok := OwnershipFromLabels(ProviderLabels("demo", "web", nil))
	if !ok || namespace != "demo" || server != "web" {
		t.Fatalf("got %q %q ok=%t", namespace, server, ok)
	}
	if _, _, ok := OwnershipFromLabels(map[string]string{ManagedByKey: "other", ProviderTagNamespaceKey: "demo", ProviderTagServerKey: "web"}); ok {
		t.Fatal("unmanaged labels should be rejected")
	}
	if _, _, ok := OwnershipFromLabels(map[string]string{ManagedByKey: ManagedByValue, ProviderTagNamespaceKey: "demo"}); ok {
		t.Fatal("incomplete labels should be rejected")
	}
}

func TestLiveLabelsMatchAcceptsCanonicalAndPortableLabels(t *testing.T) {
	if !LiveLabelsMatch(map[string]string{ManagedByKey: ManagedByValue, NamespaceKey: "prod", ServerKey: "web"}, "prod", "web") {
		t.Fatal("canonical labels should match for legacy resources")
	}
	if !LiveLabelsMatch(map[string]string{ManagedByKey: ManagedByValue, ProviderTagNamespaceKey: "prod", ProviderTagServerKey: "web"}, "prod", "web") {
		t.Fatal("portable labels should match")
	}
	if LiveLabelsMatch(map[string]string{ManagedByKey: ManagedByValue, "project": "prod", "server": "web"}, "prod", "web") {
		t.Fatal("ambiguous labels should not match")
	}
	if LiveLabelsMatch(map[string]string{ManagedByKey: ManagedByValue, ProviderTagNamespaceKey: "other", ProviderTagServerKey: "web"}, "prod", "web") {
		t.Fatal("mismatched namespace should fail")
	}
}

func TestProviderTagsUsePortableReversibleOwnershipConvention(t *testing.T) {
	labels := ProviderLabels("prod.example", "web", map[string]string{"team.name": "ops.sre"})
	tags := ProviderTags(labels)
	assertHasTag(t, tags, "managed-by:serverpro")
	assertHasTag(t, tags, "serverpro-server:web")
	for _, tag := range tags {
		for _, char := range []string{".", " "} {
			if strings.Contains(tag, char) {
				t.Fatalf("tag %q contains non-portable char %q in %+v", tag, char, tags)
			}
		}
	}
	decoded := LabelsFromProviderTags(tags)
	if decoded[ManagedByKey] != ManagedByValue || decoded[NamespaceKey] != "prod.example" || decoded[ServerKey] != "web" || decoded["team-name"] != "ops.sre" {
		t.Fatalf("tags did not decode: labels=%+v tags=%+v", decoded, tags)
	}
}

func TestProviderTagsSanitizeColonInKeys(t *testing.T) {
	labels := ProviderLabels("prod", "web", map[string]string{"team:owner": "ops"})
	tags := ProviderTags(labels)
	assertHasTag(t, tags, "team-owner:ops")
	if strings.Contains(strings.Join(tags, ","), "team:owner:ops") {
		t.Fatalf("provider tag key kept ambiguous colon: %+v", tags)
	}
	decoded := LabelsFromProviderTags(tags)
	if decoded["team-owner"] != "ops" || decoded["team"] != "" {
		t.Fatalf("ambiguous tag key decoded incorrectly: tags=%+v labels=%+v", tags, decoded)
	}
}

func TestLabelsFromProviderTagsRestoresCanonicalOwnership(t *testing.T) {
	labels := LabelsFromProviderTags([]string{"managed-by:serverpro", "serverpro-namespace:prod", "serverpro-server:web"})
	if labels[ManagedByKey] != ManagedByValue || labels[NamespaceKey] != "prod" || labels[ServerKey] != "web" {
		t.Fatalf("labels = %#v", labels)
	}
}

func TestLiveLabelsMatchDecodesEncodedPortableLabels(t *testing.T) {
	labels := ProviderLabels("prod.example", "web", nil)
	tags := ProviderTags(labels)
	decoded := LabelsFromProviderTags(tags)
	if !LiveLabelsMatch(decoded, "prod.example", "web") {
		t.Fatalf("encoded provider tags should match after decode: tags=%+v labels=%+v", tags, decoded)
	}
}

func assertHasTag(t *testing.T, tags []string, want string) {
	t.Helper()
	if slices.Contains(tags, want) {
		return
	}
	t.Fatalf("missing tag %q in %+v", want, tags)
}
