package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/credentials"
	"github.com/sagmans/serverpro/internal/ingress"
	"github.com/sagmans/serverpro/internal/mesh"
	"github.com/sagmans/serverpro/internal/poll"
	"github.com/sagmans/serverpro/internal/provider/httpjson"
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
	Devices(context.Context) ([]mesh.Device, error)
	AuthKeys(context.Context) ([]mesh.AuthKey, error)
	DeleteDevice(context.Context, string) error
	DeleteAuthKey(context.Context, string) error
}

type cleanupCloudflareClient interface {
	GetTunnel(context.Context, string) (ingress.Tunnel, error)
	DeleteTunnel(context.Context, string) error
}

const deleteTunnelActiveConnectionRetryDelay = 5 * time.Second

var (
	errTunnelActiveConnections         = errors.New("tunnel has active connections")
	errTailscaleCleanupClientRequired  = errors.New("tailscale cleanup client required")
	errCloudflareCleanupClientRequired = errors.New("cloudflare cleanup client required")
)

type serverCleanupClients struct {
	Tailscale  cleanupTailscaleClient
	Cloudflare cleanupCloudflareClient
	wait       poll.WaitFunc
}

func (a *app) prepareServerDeleteCleanup(name, stPath string, st state.State) (serverDeleteCleanup, error) {
	cleanup := serverDeleteCleanup{Required: serverDeleteCleanupRequired(st), StatePath: stPath, State: st}
	if !cleanup.Required {
		return cleanup, nil
	}
	cfg, loadedStatePath, loadedState, err := a.loadConfigAndStateForServer(name)
	if err != nil {
		return cleanup, err
	}
	if config.Expand(loadedStatePath) != config.Expand(stPath) || !sameServerDeleteAuthority(st, loadedState) {
		return cleanup, fmt.Errorf("server destructive authority changed while resolving cleanup resources; rerun delete to review current resources")
	}
	creds, _, err := a.ensureCredentials(cleanupCredentialConfig(cfg, st))
	if err != nil {
		return cleanup, err
	}
	cleanup.Config = cfg
	cleanup.Creds = creds
	return cleanup, nil
}

func serverDeleteCleanupRequired(st state.State) bool {
	return st.Cloudflare.OwnsTunnel() || st.Tailscale.NodeID != "" || st.Tailscale.AuthKeyID != ""
}

func cleanupCredentialConfig(cfg config.Config, st state.State) config.Config {
	if st.Tailscale.NodeID != "" || st.Tailscale.AuthKeyID != "" {
		cfg.Access.Tailscale.Enabled = true
		if len(cfg.Access.Tailscale.Tags) == 0 {
			cfg.Access.Tailscale.Tags = st.Tailscale.Tags
		}
	}
	if st.Cloudflare.OwnsTunnel() {
		cfg.Cloudflare.Tunnel.Enabled = true
		cfg.Cloudflare.Tunnel.CreateConnectorOnly = true
		if cfg.Cloudflare.Tunnel.Name == "" {
			cfg.Cloudflare.Tunnel.Name = st.Cloudflare.Name
		}
	}
	return cfg
}

func (a *app) preflightTrackedExternalResources(ctx context.Context, cleanup serverDeleteCleanup) error {
	clients := newServerCleanupClients(cleanup)
	if a.services.cleanupClients != nil {
		clients = a.services.cleanupClients(cleanup)
	}
	return preflightTrackedExternalResources(ctx, cleanup, clients)
}

func preflightTrackedExternalResources(ctx context.Context, cleanup serverDeleteCleanup, clients serverCleanupClients) error {
	ctx, cancel := contextWithDefaultTimeout(ctx, defaultServerOperationTimeout)
	defer cancel()
	st := cleanup.State
	if st.Tailscale.NodeID != "" {
		if clients.Tailscale == nil {
			return errTailscaleCleanupClientRequired
		}
		if _, err := validateTrackedTailscaleDevice(ctx, cleanup.Config, st, clients.Tailscale); err != nil {
			return err
		}
	}
	if st.Cloudflare.OwnsTunnel() {
		if clients.Cloudflare == nil {
			return errCloudflareCleanupClientRequired
		}
		if _, err := validateTrackedTunnel(ctx, cleanup.Config, st, clients.Cloudflare); err != nil {
			return err
		}
	}
	if st.Tailscale.AuthKeyID != "" {
		if clients.Tailscale == nil {
			return errTailscaleCleanupClientRequired
		}
		if _, err := validateTrackedAuthKey(ctx, cleanup.Config, st, clients.Tailscale); err != nil {
			return err
		}
	}
	return nil
}

func (a *app) deleteTrackedExternalResources(ctx context.Context, cleanup serverDeleteCleanup) (state.State, error) {
	if a.services.deleteTrackedExternalResources != nil {
		return a.services.deleteTrackedExternalResources(ctx, cleanup)
	}
	clients := newServerCleanupClients(cleanup)
	if a.services.cleanupClients != nil {
		clients = a.services.cleanupClients(cleanup)
	}
	return deleteTrackedExternalResources(ctx, cleanup, clients)
}

func deleteTrackedExternalResources(ctx context.Context, cleanup serverDeleteCleanup, clients serverCleanupClients) (state.State, error) {
	ctx, cancel := contextWithDefaultTimeout(ctx, defaultServerOperationTimeout)
	defer cancel()
	st := cleanup.State
	for _, deleteResource := range []func(context.Context, serverDeleteCleanup, serverCleanupClients, *state.State) error{
		deleteTrackedTailscaleDevice,
		deleteTrackedCloudflareTunnel,
		deleteTrackedTailscaleAuthKey,
	} {
		if err := deleteResource(ctx, cleanup, clients, &st); err != nil {
			return st, err
		}
	}
	return st, nil
}

func deleteTrackedTailscaleDevice(ctx context.Context, cleanup serverDeleteCleanup, clients serverCleanupClients, st *state.State) error {
	if st.Tailscale.NodeID == "" {
		return nil
	}
	if clients.Tailscale == nil {
		return errTailscaleCleanupClientRequired
	}
	found, err := validateTrackedTailscaleDevice(ctx, cleanup.Config, *st, clients.Tailscale)
	if err != nil {
		return err
	}
	if found {
		if err := clients.Tailscale.DeleteDevice(ctx, st.Tailscale.NodeID); err != nil && !httpjson.IsStatus(err, http.StatusNotFound) {
			return err
		}
	}
	// Checkpoint each external mutation so retries never repeat completed work.
	return checkpointCleanupState(cleanup.StatePath, st, clearTailscaleDeviceState)
}

func deleteTrackedCloudflareTunnel(ctx context.Context, cleanup serverDeleteCleanup, clients serverCleanupClients, st *state.State) error {
	if !st.Cloudflare.OwnsTunnel() {
		return nil
	}
	if clients.Cloudflare == nil {
		return errCloudflareCleanupClientRequired
	}
	found, err := validateTrackedTunnel(ctx, cleanup.Config, *st, clients.Cloudflare)
	if err != nil {
		return err
	}
	if found {
		if err := deleteTrackedTunnel(ctx, st.Cloudflare.TunnelID, clients.Cloudflare, clients.wait); err != nil {
			return err
		}
	}
	return checkpointCleanupState(cleanup.StatePath, st, clearTunnelState)
}

func deleteTrackedTailscaleAuthKey(ctx context.Context, cleanup serverDeleteCleanup, clients serverCleanupClients, st *state.State) error {
	if st.Tailscale.AuthKeyID == "" {
		return nil
	}
	if clients.Tailscale == nil {
		return errTailscaleCleanupClientRequired
	}
	found, err := validateTrackedAuthKey(ctx, cleanup.Config, *st, clients.Tailscale)
	if err != nil {
		return err
	}
	if found {
		if err := clients.Tailscale.DeleteAuthKey(ctx, st.Tailscale.AuthKeyID); err != nil && !httpjson.IsStatus(err, http.StatusNotFound) {
			return err
		}
	}
	return checkpointCleanupState(cleanup.StatePath, st, clearTailscaleAuthKeyState)
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

func validateTailscaleDeviceOwnership(cfg config.Config, st state.State, device mesh.Device) error {
	expectedName := st.Tailscale.Name
	if expectedName == "" {
		expectedName = cfg.Compute.Name
	}
	if expectedName == "" {
		return fmt.Errorf("tailscale ownership mismatch: state device name missing")
	}
	if !mesh.DeviceMatches(device, expectedName, nil) {
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

func validateAuthKeyOwnership(cfg config.Config, st state.State, key mesh.AuthKey) error {
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

func deleteTrackedTunnel(ctx context.Context, tunnelID string, client cleanupCloudflareClient, wait poll.WaitFunc) error {
	for {
		err := client.DeleteTunnel(ctx, tunnelID)
		if err == nil || httpjson.IsStatus(err, http.StatusNotFound) {
			return nil
		}
		if !errors.Is(err, errTunnelActiveConnections) {
			return err
		}
		if waitErr := poll.Wait(ctx, wait, deleteTunnelActiveConnectionRetryDelay); waitErr != nil {
			return fmt.Errorf("cloudflare tunnel still has active connections after waiting: %w; last delete error: %w", waitErr, err)
		}
	}
}

func checkpointCleanupState(path string, st *state.State, mutate func(*state.State)) error {
	return state.Update(config.Expand(path), func(current *state.State) error {
		mutate(current)
		*st = *current
		return nil
	})
}

func clearTailscaleDeviceState(st *state.State) {
	st.Tailscale.NodeID = ""
	st.Tailscale.Name = ""
	st.Tailscale.IPs = nil
	st.Tailscale.Tags = nil
}

func clearTailscaleAuthKeyState(st *state.State) {
	st.Tailscale.AuthKeyID = ""
}

func clearTunnelState(st *state.State) {
	st.Cloudflare = state.CloudflareState{}
}
