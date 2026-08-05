package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/assagman/serverpro/internal/provider/httpjson"
	"github.com/assagman/serverpro/internal/state"
)

// authKeyCleanupTimeout bounds best-effort deletion of the one-off bootstrap
// auth key so cleanup cannot hang the caller.
const (
	authKeyCleanupTimeout     = 10 * time.Second
	defaultComputeAccountName = "default"
)

// cleanupProvisionAuthKey deletes the one-off bootstrap auth key on a fresh,
// bounded context so it still runs when the provisioning context is already at
// or past its deadline (the key is created with a 30m expiry that can elapse
// during a slow bootstrap). A not-found result is treated as success because the
// one-off key may have expired or been garbage-collected; failing on it would
// wrongly report a completed provision as failed.
func cleanupProvisionAuthKey(st *state.State, stPath string, c TailscaleClient, keyID string) error {
	if keyID == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), authKeyCleanupTimeout)
	defer cancel()
	if err := c.DeleteAuthKey(ctx, keyID); err != nil && !httpjson.IsStatus(err, http.StatusNotFound) {
		return err
	}
	st.Tailscale.AuthKeyID = ""
	return state.Save(stPath, *st)
}

func Run(ctx context.Context, opt Options) (state.State, error) {
	cfg := opt.Config
	if opt.Clients.Compute == nil {
		return state.State{}, errors.New("compute provider required")
	}
	account := opt.ComputeAccount
	if account.Name == "" {
		account.Name = defaultComputeAccountName
	}
	if account.Provider == "" {
		account.Provider = opt.Clients.Compute.Name()
	}
	st, err := initializeProvisionState(opt.StatePath, cfg)
	if err != nil {
		return st, err
	}
	if err := checkpointProvisionIntent(opt.StatePath, &st, cfg, account); err != nil {
		return st, err
	}
	if err := ensureTailscaleTailnetIdentity(ctx, &st, opt.StatePath, opt.Creds, cfg, opt.Clients.Tailscale); err != nil {
		return st, err
	}
	if staleKeyID := st.Tailscale.AuthKeyID; staleKeyID != "" {
		// A persisted key identifies unfinished cleanup. Replacing it first would
		// lose the only durable handle needed to revoke the old credential.
		if err := cleanupProvisionAuthKey(&st, opt.StatePath, opt.Clients.Tailscale, staleKeyID); err != nil {
			return st, fmt.Errorf("retry stale tailscale auth key cleanup: %w", err)
		}
	}
	if err := captureTailscaleDeviceBaseline(ctx, &st, opt.StatePath, opt.Creds, cfg, opt.Clients.Tailscale); err != nil {
		return st, err
	}
	if err := ensureTailscalePolicy(ctx, &st, opt.StatePath, opt.Clients.Tailscale, opt.Creds, cfg); err != nil {
		return st, err
	}
	if err := ensureCloudflareTunnel(ctx, &st, opt.StatePath, cfg, opt.Clients.Cloudflare); err != nil {
		return st, err
	}
	key, keyID, err := tailscaleAuthKey(ctx, opt.Clients.Tailscale, opt.Creds, cfg)
	if err != nil {
		return st, err
	}
	if keyID != "" {
		// Best-effort cleanup for early-return failure paths; keyID is cleared once
		// the happy-path cleanup below succeeds, making this deferred call a no-op.
		defer func() { _ = cleanupProvisionAuthKey(&st, opt.StatePath, opt.Clients.Tailscale, keyID) }()
		st.Tailscale.AuthKeyID = keyID
		if err := state.Save(opt.StatePath, st); err != nil {
			return st, err
		}
	}
	if err := validateTailscaleSSHPolicy(ctx, opt.Clients.Tailscale, opt.Creds, cfg); err != nil {
		return st, err
	}
	userData, err := renderProvisionUserData(cfg, key, opt.AdminPasswordHash)
	if err != nil {
		return st, err
	}
	if err := ensureComputeServer(ctx, &st, opt.StatePath, cfg, account, opt.Clients.Compute, userData); err != nil {
		return st, err
	}
	if err := waitTailscaleDevice(ctx, &st, opt.StatePath, opt.Creds, cfg, opt.Clients.Tailscale); err != nil {
		return st, err
	}
	if err := waitRemoteReady(ctx, opt.Clients.Remote, cfg, st); err != nil {
		return st, err
	}
	if err := bootstrapRemoteNetwork(ctx, opt.Clients.Remote, opt.Clients.Cloudflare, cfg, st); err != nil {
		return st, err
	}
	// Auth-key cleanup is best-effort: the server is created and bootstrapped, so
	// a cleanup failure must not report the whole provision as failed. On success
	// keyID is cleared so the deferred fallback is a no-op; on failure the key id
	// stays in state for later retry and the one-off key expires on its own.
	if err := cleanupProvisionAuthKey(&st, opt.StatePath, opt.Clients.Tailscale, keyID); err == nil {
		keyID = ""
	}
	if err := completeProvision(opt.StatePath, &st); err != nil {
		return st, err
	}
	return st, nil
}
