package lifecycle

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/assagman/serverpro/internal/bootstraptools"
	"github.com/assagman/serverpro/internal/config"
	"github.com/assagman/serverpro/internal/network"
	"github.com/assagman/serverpro/internal/remote"
	"github.com/assagman/serverpro/internal/state"
	"github.com/assagman/serverpro/internal/tunnel"
)

func waitRemoteReady(ctx context.Context, r remote.Runner, cfg config.Config, st state.State) error {
	if r == nil || st.Tailscale.Name == "" {
		return nil
	}
	return waitRemote(ctx, r, cfg.Admin.Username, st.Tailscale.Name)
}

func bootstrapRemoteNetwork(ctx context.Context, r remote.Runner, c CloudflareClient, cfg config.Config, st state.State) error {
	if r == nil || st.Tailscale.Name == "" {
		return nil
	}
	if cloudflareTunnelConfigured(cfg) && st.Cloudflare.TunnelID != "" {
		tok, err := c.TunnelToken(ctx, st.Cloudflare.TunnelID)
		if err != nil {
			return err
		}
		if _, err := r.Run(ctx, cfg.Admin.Username, st.Tailscale.Name, tunnel.InstallScript(tok)); err != nil {
			return err
		}
	}
	bootstrapRunner := remote.WithTimeout(r, 20*time.Minute)
	if _, err := bootstrapRunner.Run(ctx, cfg.Admin.Username, st.Tailscale.Name, bootstraptools.InstallScriptForUser(cfg.Admin.Username)); err != nil {
		return fmt.Errorf("server tool bootstrap failed: %w", err)
	}
	if cfg.Network.Egress.PhaseLockdownAfterBootstrap {
		if _, err := r.Run(ctx, cfg.Admin.Username, st.Tailscale.Name, network.LockdownScript(cfg)); err != nil {
			return err
		}
	}
	return nil
}

func waitRemote(ctx context.Context, r remote.Runner, user, host string) error {
	for {
		if _, err := r.Run(ctx, user, host, "true"); err == nil {
			return nil
		} else if sudoAuthError(err) {
			return fmt.Errorf("tailscale SSH sudo validation failed: %w", err)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("tailscale SSH validation failed: %w", ctx.Err())
		case <-time.After(10 * time.Second):
		}
	}
}

func sudoAuthError(err error) bool {
	if err == nil {
		return false
	}
	evidence := strings.ToLower(err.Error())
	for _, marker := range []string{
		"sudo password required",
		"bad sudo password",
		"sorry, try again",
		"incorrect password",
		"authentication failure",
		"no password was provided",
		"a password is required",
		"a terminal is required",
	} {
		if strings.Contains(evidence, marker) {
			return true
		}
	}
	return false
}
