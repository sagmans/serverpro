package cli

import (
	"fmt"
	"slices"

	"github.com/assagman/serverpro/internal/compute"
	"github.com/assagman/serverpro/internal/config"
	"github.com/assagman/serverpro/internal/credentials"
)

func (a *app) ensureCredentials(cfg config.Config) (credentials.Set, bool, error) {
	if err := cfg.Validate(); err != nil {
		return credentials.Set{}, false, err
	}
	creds, err := credentials.LoadPartial(cfg)
	if err != nil {
		return creds, false, err
	}
	usedCachedProvider := a.applyCachedServerProviderCredential(&creds)
	missing := creds.MissingForConfig(cfg)
	if len(missing) == 0 {
		if usedCachedProvider {
			if err := credentials.Save(cfg, creds); err != nil {
				return creds, false, err
			}
			_, _ = fmt.Fprintf(a.promptWriter(), "auth saved: %s\n", config.Expand(cfg.Credentials.JSONPath))
			return creds, true, nil
		}
		return creds, false, nil
	}
	if a.nonInteractive {
		return creds, false, creds.ValidateForConfig(cfg)
	}
	_, _ = fmt.Fprintf(a.promptWriter(), "auth required; service tokens stay local at %s\n", config.Expand(cfg.Credentials.JSONPath))
	if credentialMissing(missing, "server provider API token") {
		creds.ServerProvider, err = a.promptSecret("server provider API token")
		if err != nil {
			return creds, false, err
		}
	}
	if credentialMissing(missing, "Tailscale API token") {
		creds.Tailscale, err = a.promptSecret("Tailscale API token")
		if err != nil {
			return creds, false, err
		}
	}
	if credentialMissing(missing, "Cloudflare API token") {
		creds.Cloudflare, err = a.promptSecret("Cloudflare API token")
		if err != nil {
			return creds, false, err
		}
	}
	if err := creds.ValidateForConfig(cfg); err != nil {
		return creds, false, err
	}
	if err := credentials.Save(cfg, creds); err != nil {
		return creds, false, err
	}
	_, _ = fmt.Fprintf(a.promptWriter(), "auth saved: %s\n", config.Expand(cfg.Credentials.JSONPath))
	return creds, true, nil
}

func (a *app) applyCachedServerProviderCredential(creds *credentials.Set) bool {
	if creds.ServerProvider != "" || a.provider == "" {
		return false
	}
	account, ok := a.cachedEphemeralComputeAccount(compute.ProviderName(a.provider))
	if !ok {
		return false
	}
	creds.ServerProvider = account.Token
	return true
}

func credentialMissing(missing []string, name string) bool {
	return slices.Contains(missing, name)
}
