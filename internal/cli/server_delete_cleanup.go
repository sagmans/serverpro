package cli

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/assagman/serverpro/internal/config"
	"github.com/assagman/serverpro/internal/credentials"
	"github.com/assagman/serverpro/internal/provider/cloudflare"
	"github.com/assagman/serverpro/internal/provider/httpjson"
	"github.com/assagman/serverpro/internal/provider/tailscale"
	"github.com/assagman/serverpro/internal/state"
)

type serverDeleteCleanup struct {
	Required  bool
	Config    config.Config
	StatePath string
	State     state.State
	Creds     credentials.Set
}

type cleanupTailscaleClient interface {
	Devices(context.Context) ([]tailscale.Device, error)
	AuthKeys(context.Context) ([]tailscale.AuthKey, error)
	DeleteDevice(context.Context, string) error
	DeleteAuthKey(context.Context, string) error
	RemoveServerproPolicyParts(context.Context, []string, []string, string) (bool, error)
}

type cleanupCloudflareClient interface {
	GetTunnel(context.Context, string) (cloudflare.Tunnel, error)
	DeleteTunnel(context.Context, string) error
}

type serverCleanupClients struct {
	Tailscale  cleanupTailscaleClient
	Cloudflare cleanupCloudflareClient
}

var deleteTunnelActiveConnectionRetryDelay = 5 * time.Second

func (a *app) prepareServerDeleteCleanup(name, stPath string, st state.State) (serverDeleteCleanup, error) {
	cleanup := serverDeleteCleanup{Required: serverDeleteCleanupRequired(st), StatePath: stPath, State: st}
	if !cleanup.Required {
		return cleanup, nil
	}
	cfg, loadedStatePath, loadedState, err := a.loadConfigAndStateForServer(name)
	if err != nil {
		return cleanup, err
	}
	creds, _, err := a.ensureCredentials(cleanupCredentialConfig(cfg, loadedState))
	if err != nil {
		return cleanup, err
	}
	cleanup.Config = cfg
	cleanup.StatePath = loadedStatePath
	cleanup.State = loadedState
	cleanup.Creds = creds
	return cleanup, nil
}

func serverDeleteCleanupRequired(st state.State) bool {
	return st.Cloudflare.TunnelID != "" || st.Tailscale.NodeID != "" || st.Tailscale.AuthKeyID != "" || len(st.Tailscale.PolicyTagOwners) > 0 || st.Tailscale.PolicySSHRule
}

func cleanupCredentialConfig(cfg config.Config, st state.State) config.Config {
	if st.Tailscale.NodeID != "" || st.Tailscale.AuthKeyID != "" || len(st.Tailscale.PolicyTagOwners) > 0 || st.Tailscale.PolicySSHRule {
		cfg.Access.Tailscale.Enabled = true
		if len(cfg.Access.Tailscale.Tags) == 0 {
			cfg.Access.Tailscale.Tags = st.Tailscale.Tags
		}
	}
	if st.Cloudflare.TunnelID != "" {
		cfg.Cloudflare.Tunnel.Enabled = true
		cfg.Cloudflare.Tunnel.CreateConnectorOnly = true
		cfg.Cloudflare.Tunnel.SmokeRoute = config.SmokeRoute{}
		if cfg.Cloudflare.Tunnel.Name == "" {
			cfg.Cloudflare.Tunnel.Name = st.Cloudflare.Name
		}
	}
	return cfg
}

func (a *app) deleteTrackedExternalResources(ctx context.Context, cleanup serverDeleteCleanup) (state.State, error) {
	if a.services.deleteTrackedExternalResources != nil {
		return a.services.deleteTrackedExternalResources(ctx, cleanup)
	}
	clients := serverCleanupClients{
		Tailscale:  tailscale.New(cleanup.Creds.Tailscale, cleanup.Config.Access.Tailscale.Tailnet),
		Cloudflare: cloudflare.New(cleanup.Creds.Cloudflare, cleanup.Config.Cloudflare.AccountID),
	}
	return deleteTrackedExternalResources(ctx, cleanup, clients)
}

func deleteTrackedExternalResources(ctx context.Context, cleanup serverDeleteCleanup, clients serverCleanupClients) (state.State, error) {
	ctx, cancel := contextWithDefaultTimeout(ctx, defaultServerOperationTimeout)
	defer cancel()
	st := cleanup.State
	if st.Tailscale.NodeID != "" {
		if clients.Tailscale == nil {
			return st, fmt.Errorf("tailscale cleanup client required")
		}
		found, err := validateTrackedTailscaleDevice(ctx, cleanup.Config, st, clients.Tailscale)
		if err != nil {
			return st, err
		}
		if found {
			if err := clients.Tailscale.DeleteDevice(ctx, st.Tailscale.NodeID); err != nil && !httpjson.IsStatus(err, http.StatusNotFound) {
				return st, err
			}
		}
		clearTailscaleDeviceState(&st)
		if err := state.Save(config.Expand(cleanup.StatePath), st); err != nil {
			return st, err
		}
	}
	if st.Cloudflare.TunnelID != "" {
		if clients.Cloudflare == nil {
			return st, fmt.Errorf("cloudflare cleanup client required")
		}
		found, err := validateTrackedTunnel(ctx, cleanup.Config, st, clients.Cloudflare)
		if err != nil {
			return st, err
		}
		if found {
			if err := deleteTrackedTunnel(ctx, st.Cloudflare.TunnelID, clients.Cloudflare); err != nil {
				return st, err
			}
		}
		clearTunnelState(&st)
		if err := state.Save(config.Expand(cleanup.StatePath), st); err != nil {
			return st, err
		}
	}
	if st.Tailscale.AuthKeyID != "" {
		if clients.Tailscale == nil {
			return st, fmt.Errorf("tailscale cleanup client required")
		}
		found, err := validateTrackedAuthKey(ctx, cleanup.Config, st, clients.Tailscale)
		if err != nil {
			return st, err
		}
		if found {
			if err := clients.Tailscale.DeleteAuthKey(ctx, st.Tailscale.AuthKeyID); err != nil && !httpjson.IsStatus(err, http.StatusNotFound) {
				return st, err
			}
		}
		clearTailscaleAuthKeyState(&st)
		if err := state.Save(config.Expand(cleanup.StatePath), st); err != nil {
			return st, err
		}
	}
	if len(st.Tailscale.PolicyTagOwners) > 0 || st.Tailscale.PolicySSHRule {
		if clients.Tailscale == nil {
			return st, fmt.Errorf("tailscale cleanup client required")
		}
		plan, err := tailscalePolicyRemovalPlan(cleanup, st)
		if err != nil {
			return st, err
		}
		if err := transferSharedTailscalePolicyOwnership(cleanup, st, plan); err != nil {
			return st, err
		}
		if len(plan.TagOwners) > 0 || len(plan.SSHTags) > 0 {
			if _, err := clients.Tailscale.RemoveServerproPolicyParts(ctx, plan.TagOwners, plan.SSHTags, cleanup.Config.Admin.Username); err != nil {
				return st, err
			}
		}
		clearTailscalePolicyState(&st)
		if err := state.Save(config.Expand(cleanup.StatePath), st); err != nil {
			return st, err
		}
	}
	return st, nil
}

func validateTrackedTailscaleDevice(ctx context.Context, cfg config.Config, st state.State, client cleanupTailscaleClient) (bool, error) {
	devices, err := client.Devices(ctx)
	if err != nil {
		return false, err
	}
	for _, device := range devices {
		if device.ID != st.Tailscale.NodeID && device.NodeID != st.Tailscale.NodeID {
			continue
		}
		if err := validateTailscaleDeviceOwnership(cfg, st, device); err != nil {
			return false, err
		}
		return true, nil
	}
	return false, nil
}

func validateTailscaleDeviceOwnership(cfg config.Config, st state.State, device tailscale.Device) error {
	expectedName := st.Tailscale.Name
	if expectedName == "" {
		expectedName = cfg.Compute.Name
	}
	if expectedName == "" {
		return fmt.Errorf("tailscale ownership mismatch: state device name missing")
	}
	if !tailscaleDeviceNameMatches(device, expectedName) {
		return fmt.Errorf("tailscale ownership mismatch: live device name %q hostname %q state %q", device.Name, device.Hostname, expectedName)
	}
	expectedTags := st.Tailscale.Tags
	if len(expectedTags) == 0 {
		expectedTags = cfg.Access.Tailscale.Tags
	}
	for _, tag := range expectedTags {
		if !slices.Contains(device.Tags, tag) {
			return fmt.Errorf("tailscale ownership mismatch: live device missing tag %q", tag)
		}
	}
	return nil
}

func tailscaleDeviceNameMatches(device tailscale.Device, expected string) bool {
	name := strings.TrimSuffix(device.Name, ".")
	hostname := strings.TrimSuffix(device.Hostname, ".")
	return name == expected || hostname == expected || strings.HasPrefix(name, expected+".")
}

func validateTrackedAuthKey(ctx context.Context, cfg config.Config, st state.State, client cleanupTailscaleClient) (bool, error) {
	keys, err := client.AuthKeys(ctx)
	if err != nil {
		return false, err
	}
	for _, key := range keys {
		if key.ID != st.Tailscale.AuthKeyID {
			continue
		}
		if err := validateAuthKeyOwnership(cfg, st, key); err != nil {
			return false, err
		}
		return true, nil
	}
	return false, nil
}

func validateAuthKeyOwnership(cfg config.Config, st state.State, key tailscale.AuthKey) error {
	if key.Description != "serverpro bootstrap" {
		return fmt.Errorf("tailscale ownership mismatch: auth key description %q", key.Description)
	}
	expectedTags := st.Tailscale.Tags
	if len(expectedTags) == 0 {
		expectedTags = cfg.Access.Tailscale.Tags
	}
	keyTags := key.Capabilities.Devices.Create.Tags
	for _, tag := range expectedTags {
		if !slices.Contains(keyTags, tag) {
			return fmt.Errorf("tailscale ownership mismatch: auth key missing tag %q", tag)
		}
	}
	return nil
}

type tailscalePolicyRemoval struct {
	TagOwners []string
	SSHTags   []string
}

func tailscalePolicyRemovalPlan(cleanup serverDeleteCleanup, st state.State) (tailscalePolicyRemoval, error) {
	referenced, err := referencedTailscaleTags(cleanup)
	if err != nil {
		return tailscalePolicyRemoval{}, err
	}
	plan := tailscalePolicyRemoval{TagOwners: unreferencedTags(st.Tailscale.PolicyTagOwners, referenced)}
	sshTags := policySSHTags(st, cleanup.Config)
	if st.Tailscale.PolicySSHRule && len(sshTags) > 0 {
		shared, err := tailscaleSSHRuleReferencedByOtherState(cleanup, sshTags)
		if err != nil {
			return tailscalePolicyRemoval{}, err
		}
		if !shared {
			plan.SSHTags = sshTags
		}
	}
	return plan, nil
}

func transferSharedTailscalePolicyOwnership(cleanup serverDeleteCleanup, st state.State, plan tailscalePolicyRemoval) error {
	sshTags := policySSHTags(st, cleanup.Config)
	transferSSH := st.Tailscale.PolicySSHRule && len(sshTags) > 0 && len(plan.SSHTags) == 0
	var retainedTagOwners []string
	for _, tag := range st.Tailscale.PolicyTagOwners {
		if !slices.Contains(plan.TagOwners, tag) {
			retainedTagOwners = append(retainedTagOwners, tag)
		}
	}
	if len(retainedTagOwners) == 0 && !transferSSH {
		return nil
	}
	others, _, err := otherTrackedStates(cleanup)
	if err != nil {
		return err
	}
	changed := make([]bool, len(others))
	for _, tag := range retainedTagOwners {
		transferred := false
		for i := range others {
			other := &others[i].State
			if !slices.Contains(other.Tailscale.Tags, tag) && !slices.Contains(other.Tailscale.PolicyTagOwners, tag) {
				continue
			}
			if !slices.Contains(other.Tailscale.PolicyTagOwners, tag) {
				other.Tailscale.PolicyTagOwners = append(other.Tailscale.PolicyTagOwners, tag)
				changed[i] = true
			}
			transferred = true
			break
		}
		if !transferred {
			return fmt.Errorf("cannot transfer tailscale policy ownership for tag %q to a readable sibling", tag)
		}
	}
	if transferSSH {
		transferred := false
		for i := range others {
			other := &others[i].State
			if !sameStringSet(policySSHTags(*other, cleanup.Config), sshTags) {
				continue
			}
			if !other.Tailscale.PolicySSHRule || !sameStringSet(other.Tailscale.PolicySSHTags, sshTags) {
				other.Tailscale.PolicySSHRule = true
				other.Tailscale.PolicySSHTags = append([]string(nil), sshTags...)
				changed[i] = true
			}
			transferred = true
			break
		}
		if !transferred {
			return fmt.Errorf("cannot transfer tailscale SSH policy ownership to a readable sibling")
		}
	}
	for i := range others {
		if changed[i] {
			if err := state.Save(others[i].Path, others[i].State); err != nil {
				return fmt.Errorf("transfer tailscale policy ownership: %w", err)
			}
		}
	}
	return nil
}

type trackedServerState struct {
	Path  string
	State state.State
}

// otherTrackedStates returns every tracked server state except the one being
// deleted. The bool reports whether all sibling states were readable; an
// unreadable sibling (false) forces callers to fail closed so shared Tailscale
// resources are never removed on incomplete information.
func otherTrackedStates(cleanup serverDeleteCleanup) ([]trackedServerState, bool, error) {
	reg, err := state.LoadRegistry(config.RegistryPath())
	if err != nil {
		return nil, false, err
	}
	currentPath := config.Expand(cleanup.StatePath)
	var others []trackedServerState
	complete := true
	for _, entry := range reg.List("") {
		statePath := config.Expand(entry.StatePath)
		if statePath == "" || statePath == currentPath {
			continue
		}
		other, err := state.Load(statePath)
		if err != nil {
			complete = false
			continue
		}
		others = append(others, trackedServerState{Path: statePath, State: other})
	}
	return others, complete, nil
}

func referencedTailscaleTags(cleanup serverDeleteCleanup) (map[string]bool, error) {
	others, complete, err := otherTrackedStates(cleanup)
	if err != nil {
		return nil, err
	}
	referenced := map[string]bool{}
	// Fail closed: when a sibling state is unreadable, keep every tag owner the
	// deleting server tracks rather than risk removing one a sibling still uses.
	if !complete {
		for _, tag := range cleanup.State.Tailscale.PolicyTagOwners {
			referenced[tag] = true
		}
	}
	for _, other := range others {
		for _, tag := range append(other.State.Tailscale.Tags, other.State.Tailscale.PolicyTagOwners...) {
			referenced[tag] = true
		}
	}
	return referenced, nil
}

func unreferencedTags(tags []string, referenced map[string]bool) []string {
	var out []string
	for _, tag := range tags {
		if !referenced[tag] {
			out = append(out, tag)
		}
	}
	return out
}

func tailscaleSSHRuleReferencedByOtherState(cleanup serverDeleteCleanup, sshTags []string) (bool, error) {
	others, complete, err := otherTrackedStates(cleanup)
	if err != nil {
		return false, err
	}
	// Fail closed: an unreadable sibling may still depend on the shared SSH rule,
	// so keep it rather than risk locking that server out of its only admin path.
	if !complete {
		return true, nil
	}
	for _, other := range others {
		// Any sibling whose effective SSH tag set matches relies on the same rule,
		// regardless of which server first created it. Only the creating server
		// records PolicySSHRule=true; siblings reuse the rule idempotently with
		// PolicySSHRule=false, so filtering on that flag would drop a shared rule
		// still in use and break SSH for the remaining servers.
		if sameStringSet(policySSHTags(other.State, cleanup.Config), sshTags) {
			return true, nil
		}
	}
	return false, nil
}

func policySSHTags(st state.State, cfg config.Config) []string {
	if len(st.Tailscale.PolicySSHTags) > 0 {
		return st.Tailscale.PolicySSHTags
	}
	if len(st.Tailscale.Tags) > 0 {
		return st.Tailscale.Tags
	}
	return cfg.Access.Tailscale.Tags
}

func sameStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := make(map[string]int, len(a))
	for _, item := range a {
		seen[item]++
	}
	for _, item := range b {
		seen[item]--
		if seen[item] < 0 {
			return false
		}
	}
	return true
}

func validateTrackedTunnel(ctx context.Context, cfg config.Config, st state.State, client cleanupCloudflareClient) (bool, error) {
	tunnel, err := client.GetTunnel(ctx, st.Cloudflare.TunnelID)
	if httpjson.IsStatus(err, http.StatusNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if tunnel.ID != "" && tunnel.ID != st.Cloudflare.TunnelID {
		return false, fmt.Errorf("cloudflare ownership mismatch: live tunnel id %q state %q", tunnel.ID, st.Cloudflare.TunnelID)
	}
	expectedName := st.Cloudflare.Name
	if expectedName == "" {
		expectedName = cfg.Cloudflare.Tunnel.Name
	}
	if expectedName == "" {
		expectedName = cfg.Compute.Name
	}
	if expectedName == "" {
		return false, fmt.Errorf("cloudflare ownership mismatch: state tunnel name missing")
	}
	if tunnel.Name != expectedName {
		return false, fmt.Errorf("cloudflare ownership mismatch: live tunnel name %q state %q", tunnel.Name, expectedName)
	}
	return true, nil
}

func deleteTrackedTunnel(ctx context.Context, tunnelID string, client cleanupCloudflareClient) error {
	for {
		err := client.DeleteTunnel(ctx, tunnelID)
		if err == nil || httpjson.IsStatus(err, http.StatusNotFound) {
			return nil
		}
		if !cloudflare.TunnelHasActiveConnections(err) {
			return err
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("cloudflare tunnel still has active connections after waiting: %w; last delete error: %w", ctx.Err(), err)
		case <-time.After(deleteTunnelActiveConnectionRetryDelay):
		}
	}
}

func clearTailscaleDeviceState(st *state.State) {
	st.Tailscale.NodeID = ""
	st.Tailscale.Name = ""
	st.Tailscale.IPs = nil
	st.Tailscale.Tags = nil
	st.Tailscale.DeviceBaselineCaptured = false
	st.Tailscale.PreexistingDeviceIDs = nil
}

func clearTailscaleAuthKeyState(st *state.State) {
	st.Tailscale.AuthKeyID = ""
}

func clearTailscalePolicyState(st *state.State) {
	st.Tailscale.PolicyTagOwners = nil
	st.Tailscale.PolicySSHRule = false
	st.Tailscale.PolicySSHTags = nil
}

func clearTunnelState(st *state.State) {
	st.Cloudflare = state.CloudflareState{}
}
