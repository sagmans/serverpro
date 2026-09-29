package cli

import (
	"fmt"
	"slices"

	"github.com/sagmans/serverpro/internal/compute"
	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/credentials"
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
			if err := a.mergeCredentials(cfg, creds); err != nil {
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
	if err := a.mergeCredentials(cfg, creds); err != nil {
		return creds, false, err
	}
	_, _ = fmt.Fprintf(a.promptWriter(), "auth saved: %s\n", config.Expand(cfg.Credentials.JSONPath))
	return creds, true, nil
}

// mergeCredentials fills stored gaps with the values this run supplied instead
// of publishing the snapshot it loaded: a credential another serverpro run
// rotated in the meantime stays authoritative, and the persisted set is
// revalidated for completeness against the current config.
func (a *app) mergeCredentials(cfg config.Config, supplied credentials.Set) error {
	return credentials.Update(cfg, func(current *credentials.Set) error {
		if current.ServerProvider == "" {
			current.ServerProvider = supplied.ServerProvider
		}
		if current.Tailscale == "" {
			current.Tailscale = supplied.Tailscale
		}
		if current.Cloudflare == "" {
			current.Cloudflare = supplied.Cloudflare
		}
		return current.ValidateForConfig(cfg)
	})
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
