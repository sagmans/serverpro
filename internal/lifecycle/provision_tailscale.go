package lifecycle

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/credentials"
	"github.com/sagmans/serverpro/internal/mesh"
	"github.com/sagmans/serverpro/internal/state"
)

// authKeyClockSkewMargin widens the local-clock fallback for the enrolment
// window. Devices enrolled earlier than this before the key was minted cannot
// be the new server; a controller clock running further ahead only makes create
// time out, which fails closed.
const authKeyClockSkewMargin = 5 * time.Minute

func ensureTailscalePolicy(ctx context.Context, st *state.State, stPath string, c TailscaleClient, creds credentials.Set, cfg config.Config, save provisionStateSaver) error {
	if creds.Tailscale == "" {
		return nil
	}
	change, err := c.EnsureServerproPolicy(ctx, cfg.Access.Tailscale.Tags, cfg.Access.Tailscale.SSHRuleTags(), cfg.Admin.Username, cfg.Access.Tailscale.RootPolicy, cfg.Access.Tailscale.AutomationSource(), cfg.Access.Tailscale.HumanSSHAction())
	if err != nil {
		return err
	}
	before := st.Tailscale
	RecordTailscalePolicyChange(st, cfg, change)
	if !change.Changed() && before.PolicySSHAutomationSource == st.Tailscale.PolicySSHAutomationSource && slices.Equal(before.PolicySSHTags, st.Tailscale.PolicySSHTags) {
		return nil
	}
	return save(stPath, *st)
}

// RecordTailscalePolicyChange stores a successful serverpro policy write.
// Provision, ssh-policy and the doctor repair share it, so state tells
// reconcile and doctor the same story whichever command wrote the policy.
// The recorded automation source follows config after every successful
// write, also when the rule already existed (a hand-made migration or a rule
// that another server with the same tags wrote), because reconcile protects
// the automation tag by this record.
func RecordTailscalePolicyChange(st *state.State, cfg config.Config, change mesh.PolicyChange) {
	// The automation tag owner is protected by PolicySSHAutomationSource, so it
	// stays out of PolicyTagOwners: reconcile can then prune it once no server
	// records the source.
	owners := slices.DeleteFunc(slices.Clone(change.TagOwners), func(tag string) bool { return tag == mesh.AutomationSourceTag })
	st.Tailscale.PolicyTagOwners = appendMissingStrings(st.Tailscale.PolicyTagOwners, owners)
	// A server that already owns its check rule follows its ssh_tags too: a
	// successful write means the rule for those tags is in the policy now,
	// also when an operator moved it there by hand.
	if change.SSHRule || st.Tailscale.PolicySSHRule {
		st.Tailscale.PolicySSHRule = true
		st.Tailscale.PolicySSHTags = append([]string(nil), cfg.Access.Tailscale.SSHRuleTags()...)
	}
	st.Tailscale.PolicySSHAutomationSource = cfg.Access.Tailscale.AutomationSource()
	// A server created before rule tracking gets its tags recorded here, so a
	// later delete knows which accept rule it shares.
	if st.Tailscale.PolicySSHAutomationSource != "" && len(st.Tailscale.PolicySSHTags) == 0 {
		st.Tailscale.PolicySSHTags = append([]string(nil), cfg.Access.Tailscale.SSHRuleTags()...)
	}
}

// tailscaleAuthKey mints the one-off bootstrap key through the Tailscale API.
// A stored user-supplied key is refused even when an API token is present: its
// namespace scope cannot be verified from here, and honouring it conditionally
// would let one credentials file behave two different ways.
func tailscaleAuthKey(ctx context.Context, c TailscaleClient, creds credentials.Set, cfg config.Config) (mesh.AuthKey, error) {
	if creds.TSAuthKey != "" {
		return mesh.AuthKey{}, fmt.Errorf("user-supplied tailscale_auth_key cannot be verified as namespace-scoped; remove it and provision with a Tailscale API token")
	}
	if creds.Tailscale == "" {
		return mesh.AuthKey{}, fmt.Errorf("tailscale API token required")
	}
	return c.CreateAuthKey(ctx, cfg.Access.Tailscale.Tags, 30*time.Minute)
}

// authKeyCreatedAt returns the earliest time the new server's device can have
// enrolled. The control-plane mint time is preferred because device creation
// times use the same clock. Without it, the local clock minus a skew margin
// still keeps long-lived same-name devices out; a zero window would leave a
// stale online device as the only match before the new node joins.
func authKeyCreatedAt(key mesh.AuthKey, now time.Time) time.Time {
	created, err := time.Parse(time.RFC3339, key.Created)
	if err != nil {
		return now.Add(-authKeyClockSkewMargin).UTC()
	}
	return created.UTC()
}

func validateTailscaleSSHPolicy(ctx context.Context, c TailscaleClient, creds credentials.Set, cfg config.Config) error {
	if creds.Tailscale == "" {
		return nil
	}
	return c.ValidateSSHPolicy(ctx, cfg.Access.Tailscale.Tags, cfg.Access.Tailscale.SSHRuleTags(), cfg.Admin.Username, cfg.Access.Tailscale.RootPolicy, cfg.Access.Tailscale.AutomationSource(), cfg.Access.Tailscale.HumanSSHAction())
}

// waitTailscaleDevice binds create to the device this run enrolled. Bootstrap
// secrets later travel to the recorded name, so a rerun keeps the recorded
// device instead of searching again, and a first bind ignores devices that
// predate the single-use bootstrap key.
func waitTailscaleDevice(ctx context.Context, st *state.State, stPath string, creds credentials.Set, cfg config.Config, c TailscaleClient, save provisionStateSaver) error {
	if creds.Tailscale == "" {
		return nil
	}
	q := mesh.ManagedDeviceQuery(cfg.Compute.Name, cfg.Access.Tailscale.Tags, st.Tailscale.NodeID, st.Tailscale.AuthKeyCreatedAt)
	dev, err := c.WaitDevice(ctx, q)
	if err != nil {
		return err
	}
	st.Tailscale.NodeID = dev.StableID()
	st.Tailscale.Name = bestName(dev)
	st.Tailscale.IPs = dev.Addresses
	st.Tailscale.Tags = dev.Tags
	st.Tailscale.AuthKeyCreatedAt = time.Time{}
	return save(stPath, *st)
}

func appendMissingStrings(existing, additions []string) []string {
	out := append([]string(nil), existing...)
	for _, addition := range additions {
		if !slices.Contains(out, addition) {
			out = append(out, addition)
		}
	}
	return out
}

func bestName(d mesh.Device) string {
	if d.Name != "" {
		return d.Name
	}
	return d.Hostname
}
