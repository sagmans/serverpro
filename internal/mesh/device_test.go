package mesh

import (
	"errors"
	"strings"
	"testing"
	"time"
)

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

// SelectDevice guards which node receives bootstrap secrets, so these cases
// pin the fail-closed rules: stale twins drop out by enrolment time, bound
// devices never fall back to name search, and ambiguity is terminal.
func TestSelectDeviceIdentityRules(t *testing.T) {
	keyCreated := time.Date(2026, 10, 7, 10, 0, 0, 0, time.UTC)
	tags := []string{"tag:serverpro-prod"}
	stale := Device{ID: "d-stale", NodeID: "n-stale", Name: "prod-01.example.ts.net", Hostname: "prod-01", Tags: tags, Online: true, Created: "2026-10-01T09:00:00Z"}
	fresh := Device{ID: "d-fresh", NodeID: "n-fresh", Name: "prod-01-1.example.ts.net", Hostname: "prod-01", Tags: tags, Online: true, Created: "2026-10-07T10:02:00Z"}
	twin := Device{ID: "d-twin", NodeID: "n-twin", Name: "prod-01-2.example.ts.net", Hostname: "prod-01", Tags: tags, Online: true, Created: "2026-10-07T10:03:00Z"}
	undated := Device{ID: "d-undated", NodeID: "n-undated", Name: "prod-01-3.example.ts.net", Hostname: "prod-01", Tags: tags, Online: true}

	cases := []struct {
		name    string
		devices []Device
		query   DeviceQuery
		wantID  string
		wantErr error
	}{
		{"stale twin excluded by key time", []Device{stale, fresh}, DeviceQuery{Hostname: "prod-01", Tags: tags, CreatedNotBefore: keyCreated}, "n-fresh", nil},
		{"two enrolments after key are ambiguous", []Device{stale, fresh, twin}, DeviceQuery{Hostname: "prod-01", Tags: tags, CreatedNotBefore: keyCreated}, "", ErrDeviceAmbiguous},
		{"undated device fails closed under time filter", []Device{undated}, DeviceQuery{Hostname: "prod-01", Tags: tags, CreatedNotBefore: keyCreated}, "", ErrDeviceNotFound},
		{"no time filter needs a unique match", []Device{stale, fresh}, DeviceQuery{Hostname: "prod-01", Tags: tags}, "", ErrDeviceAmbiguous},
		{"no candidate yet is retryable", []Device{stale}, DeviceQuery{Hostname: "prod-01", Tags: tags, CreatedNotBefore: keyCreated}, "", ErrDeviceNotFound},
		{"bound device wins over newer twins", []Device{stale, fresh, twin}, DeviceQuery{Hostname: "prod-01", Tags: tags, NodeID: "n-fresh"}, "n-fresh", nil},
		{"bound device gone never falls back to name", []Device{stale, twin}, DeviceQuery{Hostname: "prod-01", Tags: tags, NodeID: "n-fresh"}, "", ErrBoundDeviceMissing},
		{"bound device with lost tag is rejected", []Device{{ID: "d-fresh", NodeID: "n-fresh", Hostname: "prod-01"}}, DeviceQuery{Hostname: "prod-01", Tags: tags, NodeID: "n-fresh"}, "", ErrBoundDeviceMismatch},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := SelectDevice(tc.devices, tc.query)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil || got.NodeID != tc.wantID {
				t.Fatalf("got %q, %v; want %q", got.NodeID, err, tc.wantID)
			}
		})
	}
}

func TestSelectDeviceAmbiguityNamesEveryCandidate(t *testing.T) {
	devices := []Device{
		{NodeID: "n-b", Hostname: "prod-01"},
		{NodeID: "n-a", Hostname: "prod-01"},
	}
	_, err := SelectDevice(devices, DeviceQuery{Hostname: "prod-01"})
	if err == nil || !strings.Contains(err.Error(), "n-a, n-b") {
		t.Fatalf("err = %v, want both device ids", err)
	}
}
