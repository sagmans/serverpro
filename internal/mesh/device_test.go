package mesh

import "testing"

func TestPolicyReconcilePlanEmpty(t *testing.T) {
	if !(PolicyReconcilePlan{}).Empty() {
		t.Fatal("zero plan should be empty")
	}
	if (PolicyReconcilePlan{TagOwners: []string{"tag:serverpro-prod"}}).Empty() {
		t.Fatal("planned owner should be non-empty")
	}
}

func TestDeviceMatchesNormalizesTrailingDotsAcrossFlows(t *testing.T) {
	device := Device{Name: "prod-web.example.ts.net.", Hostname: "prod-web.", Tags: []string{"tag:serverpro-prod"}}
	for _, hostname := range []string{"prod-web", "prod-web.", "prod-web.example.ts.net", "prod-web.example.ts.net."} {
		if !DeviceMatches(device, hostname, []string{"tag:serverpro-prod"}) {
			t.Fatalf("hostname %q did not match", hostname)
		}
	}
	if DeviceMatches(device, "prod-web", []string{"tag:missing"}) {
		t.Fatal("missing tag matched")
	}
}
