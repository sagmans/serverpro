package cli

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/credentials"
	"github.com/sagmans/serverpro/internal/lifecycle"
	"github.com/sagmans/serverpro/internal/provider/cloudflare"
	"github.com/sagmans/serverpro/internal/provider/httpjson"
	"github.com/sagmans/serverpro/internal/provider/tailscale"
	"github.com/sagmans/serverpro/internal/state"
)

type serverDeleteCleanup struct {
	Required  bool
	Config    config.Config
	StatePath string
	State     state.State
	Creds     credentials.Set
}

type cleanupTailscaleClient interface {
	TailnetID(context.Context) (string, error)
	Devices(context.Context) ([]tailscale.Device, error)
	AuthKeys(context.Context) ([]tailscale.AuthKey, error)
	DeleteDevice(context.Context, string) error
	DeleteAuthKey(context.Context, string) error
	InspectServerproPolicyParts(context.Context, []string, []string, string) (tailscale.ServerproPolicyChange, error)
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
	return st.Cloudflare.TunnelID != "" || tailscaleDeleteCleanupRequired(st)
}

func tailscaleDeleteCleanupRequired(st state.State) bool {
	return st.Tailscale.NodeID != "" || st.Tailscale.AuthKeyID != "" || len(st.Tailscale.PolicyTagOwners) > 0 || st.Tailscale.PolicySSHRule || tailscalePolicyOwnershipPending(st.Tailscale)
}

func tailscalePolicyOwnershipPending(st state.TailscaleState) bool {
	return len(st.PolicyPendingTagOwners) > 0 || st.PolicyPendingSSHRule
}

func trackedTailscaleSelector(st state.State) (string, error) {
	if !tailscaleDeleteCleanupRequired(st) {
		return "", nil
	}
	if strings.TrimSpace(st.Tailscale.Tailnet) == "" || strings.TrimSpace(st.Tailscale.TailnetID) == "" {
		return "", fmt.Errorf("tracked Tailscale tailnet identity missing; state must include selector and canonical ID")
	}
	return st.Tailscale.Tailnet, nil
}

func cleanupCredentialConfig(cfg config.Config, st state.State) config.Config {
	if tailscaleDeleteCleanupRequired(st) {
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

func (a *app) preflightTrackedExternalResources(ctx context.Context, cleanup *serverDeleteCleanup) error {
	if a.services.preflightTrackedExternalResources != nil {
		return a.services.preflightTrackedExternalResources(ctx, cleanup)
	}
	var clients serverCleanupClients
	if tailscaleDeleteCleanupRequired(cleanup.State) {
		selector, err := trackedTailscaleSelector(cleanup.State)
		if err != nil {
			return err
		}
		clients.Tailscale = tailscale.New(cleanup.Creds.Tailscale, selector)
	}
	if cleanup.State.Cloudflare.TunnelID != "" {
		clients.Cloudflare = cloudflare.New(cleanup.Creds.Cloudflare, cleanup.Config.Cloudflare.AccountID)
	}
	operationCtx, cancel := contextWithDefaultTimeout(ctx, defaultServerOperationTimeout)
	defer cancel()
	return validateTrackedExternalResources(operationCtx, cleanup, clients)
}

func validateTrackedExternalResources(ctx context.Context, cleanup *serverDeleteCleanup, clients serverCleanupClients) error {
	st := cleanup.State
	if tailscaleDeleteCleanupRequired(st) {
		if clients.Tailscale == nil {
			return fmt.Errorf("tailscale cleanup client required")
		}
		if err := validateTrackedTailscaleTailnet(ctx, st, clients.Tailscale); err != nil {
			return err
		}
		if err := lifecycle.ReconcilePendingTailscalePolicy(ctx, &st, config.Expand(cleanup.StatePath), clients.Tailscale); err != nil {
			return err
		}
		cleanup.State = st
		if st.Tailscale.NodeID != "" {
			if _, err := validateTrackedTailscaleDevice(ctx, cleanup.Config, st, clients.Tailscale); err != nil {
				return err
			}
		}
		if st.Tailscale.AuthKeyID != "" {
			if _, err := validateTrackedAuthKey(ctx, cleanup.Config, st, clients.Tailscale); err != nil {
				return err
			}
		}
		if len(st.Tailscale.PolicyTagOwners) > 0 || st.Tailscale.PolicySSHRule {
			plan, err := tailscalePolicyRemovalPlan(*cleanup, st)
			if err != nil {
				return err
			}
			if _, err := planSharedTailscalePolicyOwnershipTransfers(*cleanup, st, plan); err != nil {
				return err
			}
			sshTags := []string(nil)
			if st.Tailscale.PolicySSHRule {
				sshTags = st.Tailscale.PolicySSHTags
			}
			if _, err := clients.Tailscale.InspectServerproPolicyParts(ctx, st.Tailscale.PolicyTagOwners, sshTags, st.Tailscale.PolicySSHUser); err != nil {
				return err
			}
		}
	}
	if st.Cloudflare.TunnelID != "" {
		if clients.Cloudflare == nil {
			return fmt.Errorf("cloudflare cleanup client required")
		}
		if _, err := validateTrackedTunnel(ctx, cleanup.Config, st, clients.Cloudflare); err != nil {
			return err
		}
	}
	return nil
}

func (a *app) deleteTrackedExternalResources(ctx context.Context, cleanup serverDeleteCleanup) (state.State, error) {
	if a.services.deleteTrackedExternalResources != nil {
		return a.services.deleteTrackedExternalResources(ctx, cleanup)
	}
	var tailscaleClient cleanupTailscaleClient
	if tailscaleDeleteCleanupRequired(cleanup.State) {
		selector, err := trackedTailscaleSelector(cleanup.State)
		if err != nil {
			return cleanup.State, err
		}
		tailscaleClient = tailscale.New(cleanup.Creds.Tailscale, selector)
	}
	clients := serverCleanupClients{
		Tailscale:  tailscaleClient,
		Cloudflare: cloudflare.New(cleanup.Creds.Cloudflare, cleanup.Config.Cloudflare.AccountID),
	}
	return deleteTrackedExternalResources(ctx, cleanup, clients)
}

func deleteTrackedExternalResources(ctx context.Context, cleanup serverDeleteCleanup, clients serverCleanupClients) (state.State, error) {
	ctx, cancel := contextWithDefaultTimeout(ctx, defaultServerOperationTimeout)
	defer cancel()
	st := cleanup.State
	if tailscalePolicyOwnershipPending(st.Tailscale) {
		return st, fmt.Errorf("Tailscale policy ownership is pending; reconcile live policy and state before cleanup")
	}
	if tailscaleDeleteCleanupRequired(st) {
		if clients.Tailscale == nil {
			return st, fmt.Errorf("tailscale cleanup client required")
		}
		if err := validateTrackedTailscaleTailnet(ctx, st, clients.Tailscale); err != nil {
			return st, err
		}
	}
	if st.Tailscale.NodeID != "" {
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
		found, err := validateTrackedAuthKey(ctx, cleanup.Config, cleanup.State, clients.Tailscale)
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
		plan, err := tailscalePolicyRemovalPlan(cleanup, st)
		if err != nil {
			return st, err
		}
		if err := transferSharedTailscalePolicyOwnership(cleanup, st, plan); err != nil {
			return st, err
		}
		if len(plan.TagOwners) > 0 || len(plan.SSHTags) > 0 {
			if _, err := clients.Tailscale.RemoveServerproPolicyParts(ctx, plan.TagOwners, plan.SSHTags, plan.SSHUser); err != nil {
				return st, err
			}
		}
		// A nil provider result means exact tracked parts were removed or observed
		// absent from the live policy, so ownership can now be cleared safely.
		clearTailscalePolicyState(&st)
		if err := state.Save(config.Expand(cleanup.StatePath), st); err != nil {
			return st, err
		}
	}
	return st, nil
}

func validateTrackedTailscaleTailnet(ctx context.Context, st state.State, client cleanupTailscaleClient) error {
	if _, err := trackedTailscaleSelector(st); err != nil {
		return err
	}
	id, err := client.TailnetID(ctx)
	if err != nil {
		return fmt.Errorf("validate tracked Tailscale tailnet identity: %w", err)
	}
	if id != st.Tailscale.TailnetID {
		return fmt.Errorf("tracked Tailscale tailnet identity mismatch: state %q live %q", st.Tailscale.TailnetID, id)
	}
	return nil
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
	SSHUser   string
}

type tailscaleSSHRuleIdentity struct {
	TailnetID string
	Tags      []string
	User      string
}

func trackedTailscaleSSHRuleIdentity(st state.State) (tailscaleSSHRuleIdentity, error) {
	if !st.Tailscale.PolicySSHRule {
		return tailscaleSSHRuleIdentity{}, nil
	}
	if strings.TrimSpace(st.Tailscale.TailnetID) == "" || len(st.Tailscale.PolicySSHTags) == 0 || strings.TrimSpace(st.Tailscale.PolicySSHUser) == "" {
		return tailscaleSSHRuleIdentity{}, fmt.Errorf("tracked Tailscale SSH policy identity incomplete; state must include tailnet ID, tags, and user")
	}
	return tailscaleSSHRuleIdentity{TailnetID: st.Tailscale.TailnetID, Tags: append([]string(nil), st.Tailscale.PolicySSHTags...), User: st.Tailscale.PolicySSHUser}, nil
}

func tailscalePolicyRemovalPlan(cleanup serverDeleteCleanup, st state.State) (tailscalePolicyRemoval, error) {
	if _, err := trackedTailscaleSelector(st); err != nil {
		return tailscalePolicyRemoval{}, err
	}
	referenced, err := referencedTailscaleTags(cleanup, st.Tailscale.TailnetID)
	if err != nil {
		return tailscalePolicyRemoval{}, err
	}
	plan := tailscalePolicyRemoval{TagOwners: unreferencedTags(st.Tailscale.PolicyTagOwners, referenced)}
	identity, err := trackedTailscaleSSHRuleIdentity(st)
	if err != nil {
		return tailscalePolicyRemoval{}, err
	}
	if st.Tailscale.PolicySSHRule {
		shared, err := tailscaleSSHRuleReferencedByOtherState(cleanup, identity)
		if err != nil {
			return tailscalePolicyRemoval{}, err
		}
		if !shared {
			plan.SSHTags = identity.Tags
			plan.SSHUser = identity.User
		}
	}
	return plan, nil
}

func planSharedTailscalePolicyOwnershipTransfers(cleanup serverDeleteCleanup, st state.State, plan tailscalePolicyRemoval) ([]trackedServerState, error) {
	identity, err := trackedTailscaleSSHRuleIdentity(st)
	if err != nil {
		return nil, err
	}
	transferSSH := st.Tailscale.PolicySSHRule && len(plan.SSHTags) == 0
	var retainedTagOwners []string
	for _, tag := range st.Tailscale.PolicyTagOwners {
		if !slices.Contains(plan.TagOwners, tag) {
			retainedTagOwners = append(retainedTagOwners, tag)
		}
	}
	if len(retainedTagOwners) == 0 && !transferSSH {
		return nil, nil
	}
	others, _, err := otherTrackedStates(cleanup)
	if err != nil {
		return nil, err
	}
	changed := make([]bool, len(others))
	for _, tag := range retainedTagOwners {
		transferred := false
		for i := range others {
			other := &others[i].State
			if !sameTrackedTailnet(other.Tailscale, st.Tailscale) {
				continue
			}
			if !slices.Contains(other.Tailscale.Tags, tag) && !slices.Contains(other.Tailscale.PolicyTagOwners, tag) && !slices.Contains(other.Tailscale.PolicyPendingTagOwners, tag) && !slices.Contains(other.Tailscale.PolicySSHTags, tag) {
				continue
			}
			if tailscalePolicyOwnershipPending(other.Tailscale) {
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
			return nil, fmt.Errorf("cannot transfer tailscale policy ownership for tag %q to a readable sibling", tag)
		}
	}
	if transferSSH {
		transferred := false
		for i := range others {
			other := &others[i].State
			if !sameTrackedTailnet(other.Tailscale, st.Tailscale) || tailscalePolicyOwnershipPending(other.Tailscale) || !tailscaleStateMayReferenceSSHRule(*other, identity, st.NamespaceName()) {
				continue
			}
			// Never overwrite legacy ownership whose exact rule identity is unknown.
			if other.Tailscale.PolicySSHRule && other.Tailscale.PolicySSHUser == "" {
				continue
			}
			if !other.Tailscale.PolicySSHRule || !sameStringSet(other.Tailscale.PolicySSHTags, identity.Tags) || other.Tailscale.PolicySSHUser != identity.User {
				other.Tailscale.PolicySSHRule = true
				other.Tailscale.PolicySSHTags = append([]string(nil), identity.Tags...)
				other.Tailscale.PolicySSHUser = identity.User
				changed[i] = true
			}
			transferred = true
			break
		}
		if !transferred {
			return nil, fmt.Errorf("cannot transfer tailscale SSH policy ownership to a readable sibling")
		}
	}
	var transfers []trackedServerState
	for i := range others {
		if changed[i] {
			transfers = append(transfers, others[i])
		}
	}
	return transfers, nil
}

func transferSharedTailscalePolicyOwnership(cleanup serverDeleteCleanup, st state.State, plan tailscalePolicyRemoval) error {
	transfers, err := planSharedTailscalePolicyOwnershipTransfers(cleanup, st, plan)
	if err != nil {
		return err
	}
	for _, transfer := range transfers {
		if err := state.Save(transfer.Path, transfer.State); err != nil {
			return fmt.Errorf("transfer tailscale policy ownership: %w", err)
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

func referencedTailscaleTags(cleanup serverDeleteCleanup, ownerTailnetID string) (map[string]bool, error) {
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
		if other.State.Tailscale.TailnetID != "" && other.State.Tailscale.TailnetID != ownerTailnetID {
			continue
		}
		tags := append([]string(nil), other.State.Tailscale.Tags...)
		tags = append(tags, other.State.Tailscale.PolicyTagOwners...)
		tags = append(tags, other.State.Tailscale.PolicyPendingTagOwners...)
		tags = append(tags, other.State.Tailscale.PolicySSHTags...)
		for _, tag := range tags {
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

func tailscaleSSHRuleReferencedByOtherState(cleanup serverDeleteCleanup, identity tailscaleSSHRuleIdentity) (bool, error) {
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
		if tailscaleStateMayReferenceSSHRule(other.State, identity, cleanup.State.NamespaceName()) {
			return true, nil
		}
	}
	return false, nil
}

func tailscaleStateMayReferenceSSHRule(st state.State, identity tailscaleSSHRuleIdentity, ownerNamespace string) bool {
	if st.Tailscale.TailnetID != "" && st.Tailscale.TailnetID != identity.TailnetID {
		return false
	}
	if len(st.Tailscale.PolicySSHTags) > 0 {
		if !sameStringSet(st.Tailscale.PolicySSHTags, identity.Tags) {
			return false
		}
		// Missing users identify legacy consumers; retaining is safer than
		// removing an authorization rule they may still require.
		return st.Tailscale.PolicySSHUser == "" || st.Tailscale.PolicySSHUser == identity.User
	}
	if len(st.Tailscale.Tags) > 0 {
		return sameStringSet(st.Tailscale.Tags, identity.Tags)
	}
	return ownerNamespace != "" && st.NamespaceName() == ownerNamespace && sameStringSet(identity.Tags, []string{config.ProjectTailscaleTag(ownerNamespace)})
}

func sameTrackedTailnet(a, b state.TailscaleState) bool {
	return strings.TrimSpace(a.Tailnet) != "" && a.TailnetID != "" && a.TailnetID == b.TailnetID
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
	st.Tailscale.PolicyPendingTagOwners = nil
	st.Tailscale.PolicyPendingSSHRule = false
	st.Tailscale.PolicySSHTags = nil
	st.Tailscale.PolicySSHUser = ""
}

func clearTunnelState(st *state.State) {
	st.Cloudflare = state.CloudflareState{}
}
