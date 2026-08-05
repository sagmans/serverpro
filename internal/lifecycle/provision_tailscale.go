package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/credentials"
	"github.com/sagmans/serverpro/internal/provider/httpjson"
	"github.com/sagmans/serverpro/internal/provider/tailscale"
	"github.com/sagmans/serverpro/internal/state"
)

func ensureTailscaleTailnetIdentity(ctx context.Context, st *state.State, stPath string, creds credentials.Set, cfg config.Config, c TailscaleClient) error {
	if creds.Tailscale == "" {
		return nil
	}
	if st.Tailscale.Tailnet == "" || st.Tailscale.TailnetID == "" {
		if tailscaleStateTracksExternalResources(*st) {
			return fmt.Errorf("tracked Tailscale tailnet identity missing; refusing provider mutation")
		}
		id, err := c.TailnetID(ctx)
		if err != nil {
			return fmt.Errorf("resolve Tailscale tailnet identity: %w", err)
		}
		if strings.TrimSpace(id) == "" {
			return fmt.Errorf("resolved Tailscale tailnet identity is empty")
		}
		st.Tailscale.Tailnet = cfg.Access.Tailscale.Tailnet
		st.Tailscale.TailnetID = id
		return state.Save(stPath, *st)
	}
	id, err := c.TailnetID(ctx)
	if err != nil {
		return fmt.Errorf("validate Tailscale tailnet identity: %w", err)
	}
	if id != st.Tailscale.TailnetID {
		return fmt.Errorf("tracked Tailscale tailnet identity mismatch: state %q live %q", st.Tailscale.TailnetID, id)
	}
	return nil
}

func tailscaleStateTracksExternalResources(st state.State) bool {
	return st.Tailscale.NodeID != "" || st.Tailscale.AuthKeyID != "" || len(st.Tailscale.PolicyTagOwners) > 0 || st.Tailscale.PolicySSHRule || tailscalePolicyPending(st.Tailscale) || len(st.Tailscale.PolicySSHTags) > 0 || st.Tailscale.PolicySSHUser != ""
}

func tailscalePolicyPending(st state.TailscaleState) bool {
	return len(st.PolicyPendingTagOwners) > 0 || st.PolicyPendingSSHRule
}

func ReconcilePendingTailscalePolicy(ctx context.Context, st *state.State, stPath string, c TailscalePolicyInspector) error {
	if !tailscalePolicyPending(st.Tailscale) {
		return nil
	}
	sshTags := []string(nil)
	if st.Tailscale.PolicyPendingSSHRule {
		if len(st.Tailscale.PolicySSHTags) == 0 || strings.TrimSpace(st.Tailscale.PolicySSHUser) == "" {
			return fmt.Errorf("pending Tailscale SSH policy identity incomplete; refusing reconciliation")
		}
		sshTags = st.Tailscale.PolicySSHTags
	}
	present, err := c.InspectServerproPolicyParts(ctx, st.Tailscale.PolicyPendingTagOwners, sshTags, st.Tailscale.PolicySSHUser)
	if err != nil {
		return fmt.Errorf("inspect pending Tailscale policy ownership: %w", err)
	}
	pendingTags := slices.Clone(st.Tailscale.PolicyPendingTagOwners)
	presentTags := slices.Clone(present.TagOwners)
	slices.Sort(pendingTags)
	slices.Sort(presentTags)
	allPresent := slices.Equal(pendingTags, presentTags) && (!st.Tailscale.PolicyPendingSSHRule || present.SSHRule)
	nonePresent := len(presentTags) == 0 && !present.SSHRule
	if !allPresent && !nonePresent {
		return fmt.Errorf("pending Tailscale policy ownership is partially applied; reconcile live policy before retrying")
	}
	previous := st.Tailscale
	if allPresent {
		st.Tailscale.PolicyTagOwners = appendMissingStrings(st.Tailscale.PolicyTagOwners, st.Tailscale.PolicyPendingTagOwners)
		if st.Tailscale.PolicyPendingSSHRule {
			st.Tailscale.PolicySSHRule = true
		}
	}
	st.Tailscale.PolicyPendingTagOwners = nil
	st.Tailscale.PolicyPendingSSHRule = false
	if err := state.Save(stPath, *st); err != nil {
		st.Tailscale = previous
		return fmt.Errorf("save reconciled Tailscale policy ownership: %w", err)
	}
	return nil
}

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
	if err := ReconcilePendingTailscalePolicy(ctx, st, stPath, c); err != nil {
		return err
	}
	if st.Tailscale.PolicySSHRule {
		if len(st.Tailscale.PolicySSHTags) == 0 || strings.TrimSpace(st.Tailscale.PolicySSHUser) == "" {
			return fmt.Errorf("tracked Tailscale SSH policy identity incomplete; refusing provider mutation")
		}
		trackedTags := slices.Clone(st.Tailscale.PolicySSHTags)
		configuredTags := slices.Clone(cfg.Access.Tailscale.Tags)
		slices.Sort(trackedTags)
		slices.Sort(configuredTags)
		// Replacing an owned identity would orphan the old authorization rule;
		// cleanup must use the tracked identity before config can change it.
		if st.Tailscale.PolicySSHUser != cfg.Admin.Username || !slices.Equal(trackedTags, configuredTags) {
			return fmt.Errorf("tracked Tailscale SSH policy identity differs from config; clean up the tracked rule before reprovisioning")
		}
	}
	checkpointed := false
	change, err := c.EnsureServerproPolicy(ctx, cfg.Access.Tailscale.Tags, cfg.Admin.Username, cfg.Access.Tailscale.RootPolicy, func(change tailscale.ServerproPolicyChange) error {
		previous := st.Tailscale
		st.Tailscale.PolicyPendingTagOwners = append([]string(nil), change.TagOwners...)
		st.Tailscale.PolicyPendingSSHRule = change.SSHRule
		// Exact identity is durable before provider mutation, but ownership stays
		// pending until the conditional provider write confirms success.
		st.Tailscale.PolicySSHTags = append([]string(nil), cfg.Access.Tailscale.Tags...)
		st.Tailscale.PolicySSHUser = cfg.Admin.Username
		if err := state.Save(stPath, *st); err != nil {
			st.Tailscale = previous
			return err
		}
		checkpointed = true
		return nil
	})
	if err != nil {
		if checkpointed && httpjson.IsStatus(err, http.StatusPreconditionFailed) {
			pending := st.Tailscale
			st.Tailscale.PolicyPendingTagOwners = nil
			st.Tailscale.PolicyPendingSSHRule = false
			if saveErr := state.Save(stPath, *st); saveErr != nil {
				st.Tailscale = pending
				return errors.Join(err, fmt.Errorf("clear rejected Tailscale policy ownership checkpoint: %w", saveErr))
			}
		}
		return err
	}
	if !checkpointed {
		return fmt.Errorf("Tailscale policy ownership checkpoint was not executed")
	}
	if len(change.TagOwners) == 0 && !change.SSHRule {
		return nil
	}
	pending := st.Tailscale
	st.Tailscale.PolicyTagOwners = appendMissingStrings(st.Tailscale.PolicyTagOwners, change.TagOwners)
	if change.SSHRule {
		st.Tailscale.PolicySSHRule = true
	}
	st.Tailscale.PolicyPendingTagOwners = nil
	st.Tailscale.PolicyPendingSSHRule = false
	if err := state.Save(stPath, *st); err != nil {
		st.Tailscale = pending
		return fmt.Errorf("confirm Tailscale policy ownership: %w", err)
	}
	return nil
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
