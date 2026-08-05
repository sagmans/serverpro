package config

import "testing"

func TestProjectTailscaleTagEncodesDomainProject(t *testing.T) {
	if got := ProjectTailscaleTag("example.com"); got != "tag:serverpro-example-x2e-com" {
		t.Fatalf("ProjectTailscaleTag() = %q", got)
	}
}

func TestProjectTailscaleTagAvoidsSlugCollisions(t *testing.T) {
	projects := []string{"foo.bar", "foo-bar", "foo_bar", "foo-x2e-bar"}
	seen := map[string]string{}
	for _, project := range projects {
		tag := ProjectTailscaleTag(project)
		if other, ok := seen[tag]; ok {
			t.Fatalf("projects %q and %q produced same tag %q", other, project, tag)
		}
		seen[tag] = project
	}
}

func TestServerResourceNameEncodesProviderUnsafeIDs(t *testing.T) {
	if got := ServerResourceName("example.com", "api_blue"); got != "example-x2e-com-api-x5f-blue" {
		t.Fatalf("ServerResourceName() = %q", got)
	}
}
