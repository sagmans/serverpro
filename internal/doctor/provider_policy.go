package doctor

import (
	"context"
	"errors"
	"strings"
	"time"
)

const (
	tailnetPolicyCheckName = "tailnet policy"
	// tailnetPolicyFetchTimeout matches the tailnet DNS read: one small API
	// GET that must not hold up the remote checks behind it.
	tailnetPolicyFetchTimeout = 10 * time.Second
	// tailnetPolicyRemediation points at the reviewed grants example instead
	// of a fix, because the policy file is tailnet-wide and serverpro only
	// ever edits its own tag owners and SSH rule there.
	tailnetPolicyRemediation = "replace the allow-all rule with least-privilege grants (docs/tailscale-setup.md, Recommended grants policy); serverpro never edits tailnet packet rules"
)

// errTailnetPolicyUnsupported lets the snapshot wrapper, which always has the
// method, report a source client that cannot read the policy as a skip.
var errTailnetPolicyUnsupported = errors.New("tailnet policy introspection not supported")

// checkTailnetPacketPolicy warns when the tailnet policy lets every source
// reach every port. Tailscale traffic is not public ingress, yet such a rule
// leaves UFW as the only barrier between any tailnet device (including a
// compromised laptop) and every port a managed host listens on. It warns
// rather than fails because the policy is shared by the whole tailnet and
// outside what one server's doctor run can own.
func checkTailnetPacketPolicy(ctx context.Context, token string, client TailscaleClient) Result {
	if token == "" {
		return skip("provider", tailnetPolicyCheckName, "no API token; auth-key-only mode")
	}
	policyClient, ok := client.(TailnetPacketPolicyClient)
	if !ok {
		return skip("provider", tailnetPolicyCheckName, errTailnetPolicyUnsupported.Error())
	}
	tsCtx, cancel := context.WithTimeout(ctx, tailnetPolicyFetchTimeout)
	policy, err := policyClient.PacketPolicy(tsCtx)
	cancel()
	if errors.Is(err, errTailnetPolicyUnsupported) {
		return skip("provider", tailnetPolicyCheckName, err.Error())
	}
	if err != nil {
		result := warn("provider", tailnetPolicyCheckName, "policy fetch failed: "+err.Error())
		result.Remediation = "grant the Tailscale API token policy-file read access, then rerun doctor"
		return result
	}
	if rules := policy.AllowAllRules(); len(rules) > 0 {
		result := warn("provider", tailnetPolicyCheckName, "every tailnet source reaches every port: "+strings.Join(rules, "; "))
		result.Remediation = tailnetPolicyRemediation
		return result
	}
	return pass("provider", tailnetPolicyCheckName, "no allow-all acls or grants")
}
