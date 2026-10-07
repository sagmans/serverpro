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
	change, err := c.EnsureServerproPolicy(ctx, cfg.Access.Tailscale.Tags, cfg.Admin.Username, cfg.Access.Tailscale.RootPolicy)
	if err != nil {
		return err
	}
	if len(change.TagOwners) == 0 && !change.SSHRule {
		return nil
	}
	st.Tailscale.PolicyTagOwners = appendMissingStrings(st.Tailscale.PolicyTagOwners, change.TagOwners)
	if change.SSHRule {
		st.Tailscale.PolicySSHRule = true
		st.Tailscale.PolicySSHTags = append([]string(nil), cfg.Access.Tailscale.Tags...)
	}
	return save(stPath, *st)
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
	return c.ValidateSSHPolicy(ctx, cfg.Access.Tailscale.Tags, cfg.Admin.Username, cfg.Access.Tailscale.RootPolicy)
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
