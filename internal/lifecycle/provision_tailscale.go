package lifecycle

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/assagman/serverpro/internal/config"
	"github.com/assagman/serverpro/internal/credentials"
	"github.com/assagman/serverpro/internal/provider/tailscale"
	"github.com/assagman/serverpro/internal/state"
)

func captureTailscaleDeviceBaseline(ctx context.Context, st *state.State, stPath string, creds credentials.Set, cfg config.Config, c TailscaleClient) error {
	if creds.Tailscale == "" || st.Tailscale.NodeID != "" || st.Tailscale.DeviceBaselineCaptured {
		return nil
	}
	if st.Compute.ID != "" {
		return fmt.Errorf("tailscale device baseline missing for existing compute server; delete and recreate the unbound server before provisioning")
	}
	ids, err := c.MatchingDeviceIDs(ctx, cfg.Compute.Name, cfg.Access.Tailscale.Tags)
	if err != nil {
		return fmt.Errorf("tailscale device baseline failed: %w", err)
	}
	// Persisting even an empty snapshot distinguishes a safe pre-create inventory
	// from legacy state that cannot prove which matching device is newly enrolled.
	st.Tailscale.DeviceBaselineCaptured = true
	st.Tailscale.PreexistingDeviceIDs = append([]string(nil), ids...)
	return state.Save(stPath, *st)
}

func ensureTailscalePolicy(ctx context.Context, st *state.State, stPath string, c TailscaleClient, creds credentials.Set, cfg config.Config) error {
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
	return state.Save(stPath, *st)
}

func tailscaleAuthKey(ctx context.Context, c TailscaleClient, creds credentials.Set, cfg config.Config) (key string, id string, err error) {
	if creds.TSAuthKey != "" && creds.Tailscale == "" {
		return "", "", fmt.Errorf("tailscale API token required; provided auth keys cannot be verified as project-scoped")
	}
	if creds.Tailscale == "" {
		return "", "", fmt.Errorf("tailscale API token required")
	}
	created, err := c.CreateAuthKey(ctx, cfg.Access.Tailscale.Tags, 30*time.Minute)
	if err != nil {
		return "", "", err
	}
	return created.Key, created.ID, nil
}

func validateTailscaleSSHPolicy(ctx context.Context, c TailscaleClient, creds credentials.Set, cfg config.Config) error {
	if creds.Tailscale == "" {
		return nil
	}
	return c.ValidateSSHPolicy(ctx, cfg.Access.Tailscale.Tags, cfg.Admin.Username, cfg.Access.Tailscale.RootPolicy)
}

func waitTailscaleDevice(ctx context.Context, st *state.State, stPath string, creds credentials.Set, cfg config.Config, c TailscaleClient) error {
	if creds.Tailscale == "" {
		return nil
	}
	request := tailscale.DeviceWait{
		Hostname:    cfg.Compute.Name,
		Tags:        cfg.Access.Tailscale.Tags,
		ExcludedIDs: st.Tailscale.PreexistingDeviceIDs,
		DeviceID:    st.Tailscale.NodeID,
	}
	dev, err := c.WaitDevice(ctx, request)
	if err != nil {
		return err
	}
	deviceID := bestDeviceID(dev)
	if deviceID == "" {
		return fmt.Errorf("tailscale device %s has no stable device ID", cfg.Compute.Name)
	}
	if st.Tailscale.NodeID != "" && st.Tailscale.NodeID != deviceID {
		return fmt.Errorf("tailscale device binding changed from %q to %q", st.Tailscale.NodeID, deviceID)
	}
	st.Tailscale.NodeID = deviceID
	st.Tailscale.Name = bestName(dev)
	st.Tailscale.IPs = append([]string(nil), dev.Addresses...)
	st.Tailscale.Tags = append([]string(nil), dev.Tags...)
	return state.Save(stPath, *st)
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

func bestDeviceID(d tailscale.Device) string {
	if d.NodeID != "" {
		return d.NodeID
	}
	return d.ID
}

func bestName(d tailscale.Device) string {
	if d.Name != "" {
		return d.Name
	}
	return d.Hostname
}
