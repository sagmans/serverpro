package lifecycle

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/sagmans/serverpro/internal/provider/httpjson"
	"github.com/sagmans/serverpro/internal/state"
)

// authKeyCleanupTimeout bounds best-effort deletion of the one-off bootstrap
// auth key so cleanup cannot hang the caller.
const authKeyCleanupTimeout = 10 * time.Second

// cleanupProvisionAuthKey deletes the one-off bootstrap auth key on a fresh,
// bounded context so it still runs when the provisioning context is already at
// or past its deadline (the key is created with a 30m expiry that can elapse
// during a slow bootstrap). A not-found result is treated as success because the
// one-off key may have expired or been garbage-collected; failing on it would
// wrongly report a completed provision as failed.
func cleanupProvisionAuthKey(st *state.State, stPath string, c TailscaleClient, keyID string, save provisionStateSaver) error {
	if keyID == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), authKeyCleanupTimeout)
	defer cancel()
	if err := c.DeleteAuthKey(ctx, keyID); err != nil && !httpjson.IsStatus(err, http.StatusNotFound) {
		return err
	}
	st.Tailscale.AuthKeyID = ""
	return save(stPath, *st)
}

func Run(ctx context.Context, opt Options) (state.State, error) {
	cfg := opt.Config
	st, err := initializeProvisionState(opt.StatePath, cfg)
	if err != nil {
		return st, newProvisionError(ProvisionPhaseInitialize, st, err)
	}
	save := opt.stateSaver()
	if err := ensureTailscalePolicy(ctx, &st, opt.StatePath, opt.Clients.Tailscale, opt.Creds, cfg, save); err != nil {
		return st, newProvisionError(ProvisionPhaseTailscalePolicy, st, err)
	}
	if err := ensureCloudflareTunnel(ctx, &st, opt.StatePath, cfg, opt.Clients.Cloudflare, save); err != nil {
		return st, newProvisionError(ProvisionPhaseCloudflareTunnel, st, err)
	}
	keyID := st.Tailscale.AuthKeyID
	key := ""
	needsCompute := st.Compute.ID == ""
	if needsCompute {
		if err := cleanupProvisionAuthKey(&st, opt.StatePath, opt.Clients.Tailscale, keyID, save); err != nil {
			return st, newProvisionError(ProvisionPhaseTailscaleAuthKey, st, err)
		}
		key, keyID, err = tailscaleAuthKey(ctx, opt.Clients.Tailscale, opt.Creds, cfg)
		if err != nil {
			return st, newProvisionError(ProvisionPhaseTailscaleAuthKey, st, err)
		}
		if keyID != "" {
			// Fresh bootstrap keys are compensated on every early return. A key for
			// checkpointed compute remains until that server reaches device readiness.
			defer func() { _ = cleanupProvisionAuthKey(&st, opt.StatePath, opt.Clients.Tailscale, keyID, save) }()
			st.Tailscale.AuthKeyID = keyID
			if err := save(opt.StatePath, st); err != nil {
				return st, newProvisionError(ProvisionPhaseTailscaleAuthKey, st, err)
			}
		}
	}
	if err := validateTailscaleSSHPolicy(ctx, opt.Clients.Tailscale, opt.Creds, cfg); err != nil {
		return st, newProvisionError(ProvisionPhaseTailscalePolicyValidation, st, err)
	}
	userData := ""
	if needsCompute {
		userData, err = renderProvisionUserData(cfg, key, opt.AdminPasswordHash)
		if err != nil {
			return st, newProvisionError(ProvisionPhaseBootstrapRender, st, err)
		}
	}
	if opt.Clients.Compute == nil {
		return st, newProvisionError(ProvisionPhaseCompute, st, errors.New("compute provider required"))
	}
	account := opt.ComputeAccount
	if account.Name == "" {
		account.Name = "default"
	}
	if account.Provider == "" {
		account.Provider = opt.Clients.Compute.Name()
	}
	if err := ensureComputeServer(ctx, &st, opt.StatePath, cfg, account, opt.Clients.Compute, userData, save); err != nil {
		return st, newProvisionError(ProvisionPhaseCompute, st, err)
	}
	if err := waitTailscaleDevice(ctx, &st, opt.StatePath, opt.Creds, cfg, opt.Clients.Tailscale, save); err != nil {
		return st, newProvisionError(ProvisionPhaseTailscaleDevice, st, err)
	}
	if err := waitRemoteReady(ctx, opt.Clients.Remote, cfg, st); err != nil {
		return st, newProvisionError(ProvisionPhaseRemoteReady, st, err)
	}
	if err := bootstrapRemoteNetwork(ctx, opt.Clients.Remote, opt.Clients.Cloudflare, cfg, st); err != nil {
		return st, newProvisionError(ProvisionPhaseRemoteBootstrap, st, err)
	}
	// Auth-key cleanup is best-effort: the server is created and bootstrapped, so
	// a cleanup failure must not report the whole provision as failed. On success
	// keyID is cleared so the deferred fallback is a no-op; on failure the key id
	// stays in state for later retry and the one-off key expires on its own.
	if err := cleanupProvisionAuthKey(&st, opt.StatePath, opt.Clients.Tailscale, keyID, save); err == nil {
		keyID = ""
	}
	if err := completeProvision(opt.StatePath, &st, save, opt.now()); err != nil {
		return st, newProvisionError(ProvisionPhaseComplete, st, err)
	}
	return st, nil
}
