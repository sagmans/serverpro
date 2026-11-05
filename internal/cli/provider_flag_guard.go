package cli

import (
	"errors"
	"fmt"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/credentials"
	"github.com/sagmans/serverpro/internal/state"
)

// errProviderFlagMismatch marks a command stopped because its --provider value
// disagrees with the provider that owns the server's state or its stored token.
// The flag alone must never choose which vendor API receives a stored token. Its
// messages hold names and local paths only, so they are safe to print.
var errProviderFlagMismatch = errors.New("provider flag mismatch")

// validateProviderFlag fails closed before a stored server token is used. An
// empty state provider is a failure too: with nothing to compare, the flag
// would be the only evidence for sending the token to that vendor. Commands
// without the flag pass, because they already take the provider from state.
// Each error names the recovery step, because the next action differs between a
// wrong flag and state that has no provider to compare with.
func (a *app) validateProviderFlag(st state.State) error {
	if a.provider == "" || a.provider == st.Compute.Provider {
		return nil
	}
	target := st.Namespace + "/" + st.Server
	if st.Compute.Provider == "" {
		return fmt.Errorf("%w: -p %q cannot be verified: state for %s records no provider; no provider call was made; run serverpro server import %s -n %s -p <provider> --force to record it", errProviderFlagMismatch, a.provider, target, st.Server, st.Namespace)
	}
	return fmt.Errorf("%w: -p %q differs from state provider %q for %s; no provider call was made; omit -p or pass -p %s", errProviderFlagMismatch, a.provider, st.Compute.Provider, target, st.Compute.Provider)
}

// validateStoredTokenProvider fails closed when a stored token is not recorded
// for the provider about to receive it. An unrecorded token fails too, because
// the flag would then be the only evidence for that vendor. A set with no token
// passes: the prompt that follows records the provider of the new token.
func validateStoredTokenProvider(cfg config.Config, creds credentials.Set, provider string) error {
	if creds.ServerProvider == "" || creds.ServerProviderName == provider {
		return nil
	}
	target := cfg.Namespace + "/" + cfg.Server
	path := config.Expand(cfg.Credentials.JSONPath)
	if creds.ServerProviderName == "" {
		return fmt.Errorf("%w: provider %q cannot be verified: the stored token of %s records no provider; no provider call was made; if the token belongs to %s, set server_provider_name in %s; otherwise remove server_provider_token there and enter a new token", errProviderFlagMismatch, provider, target, provider, path)
	}
	return fmt.Errorf("%w: provider %q differs from provider %q recorded for the stored token of %s; no provider call was made; use provider %s, or remove server_provider_token and server_provider_name in %s and enter a new token", errProviderFlagMismatch, provider, creds.ServerProviderName, target, creds.ServerProviderName, path)
}

// validateCreateProviderAgainstState protects a create rerun and a first create
// with a stored token, which read the flag as their provider and send that
// token in preflight. State that names the flag's provider is proof, so an
// older unrecorded token gets its provider recorded here. State with no provider
// and no provider resources proves nothing, so the stored token must prove it.
func (a *app) validateCreateProviderAgainstState(cfg config.Config, stPath string) error {
	confirmed, err := a.stateConfirmsCreateProvider(stPath)
	if err != nil {
		return err
	}
	creds, err := credentials.LoadPartial(cfg)
	if err != nil {
		return err
	}
	if confirmed && creds.ServerProvider != "" && creds.ServerProviderName == "" {
		return credentials.Update(cfg, func(current *credentials.Set) error {
			if current.ServerProviderName == "" {
				current.ServerProviderName = a.provider
			}
			return nil
		})
	}
	return validateStoredTokenProvider(cfg, creds, a.provider)
}

// stateConfirmsCreateProvider reports whether state names the flag's provider.
// A recorded resource shows a provider owns it, so a flag cannot name that owner.
func (a *app) stateConfirmsCreateProvider(stPath string) (bool, error) {
	expanded := config.Expand(stPath)
	exists, err := state.Exists(expanded)
	if err != nil || !exists {
		return false, err
	}
	st, err := state.Load(expanded)
	if err != nil {
		return false, err
	}
	if st.Compute.Provider == "" && st.Compute.ID == "" && len(st.Compute.ManagedResources) == 0 {
		return false, nil
	}
	return true, a.validateProviderFlag(st)
}
