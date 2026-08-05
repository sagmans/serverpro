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

func tailscaleAuthKey(ctx context.Context, c TailscaleClient, creds credentials.Set, cfg config.Config) (key string, id string, err error) {
	if creds.TSAuthKey != "" && creds.Tailscale == "" {
		return "", "", fmt.Errorf("tailscale API token required; provided auth keys cannot be verified as namespace-scoped")
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

func waitTailscaleDevice(ctx context.Context, st *state.State, stPath string, creds credentials.Set, cfg config.Config, c TailscaleClient, save provisionStateSaver) error {
	if creds.Tailscale == "" {
		return nil
	}
	dev, err := c.WaitDevice(ctx, cfg.Compute.Name, cfg.Access.Tailscale.Tags)
	if err != nil {
		return err
	}
	st.Tailscale.NodeID = bestDeviceID(dev)
	st.Tailscale.Name = bestName(dev)
	st.Tailscale.IPs = dev.Addresses
	st.Tailscale.Tags = dev.Tags
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

func bestDeviceID(d mesh.Device) string {
	if d.NodeID != "" {
		return d.NodeID
	}
	return d.ID
}

func bestName(d mesh.Device) string {
	if d.Name != "" {
		return d.Name
	}
	return d.Hostname
}
