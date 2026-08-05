package digitalocean

import "testing"

func TestRuleTargetsEmpty(t *testing.T) {
	tests := []struct {
		name    string
		targets RuleTargets
		want    bool
	}{
		{name: "zero", want: true},
		{name: "addresses", targets: RuleTargets{Addresses: []string{"0.0.0.0/0"}}},
		{name: "tags", targets: RuleTargets{Tags: []string{"tag:web"}}},
		{name: "droplets", targets: RuleTargets{DropletIDs: []int64{1}}},
		{name: "load balancers", targets: RuleTargets{LoadBalancerUIDs: []string{"lb-1"}}},
		{name: "kubernetes", targets: RuleTargets{KubernetesIDs: []string{"cluster-1"}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.targets.empty(); got != test.want {
				t.Fatalf("empty() = %t, want %t", got, test.want)
			}
		})
	}
}
