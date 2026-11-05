package cli

import (
	"errors"
	"fmt"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/state"
)

// errProviderFlagMismatch marks a command stopped because its --provider value
// disagrees with the provider that owns the server's state. Server credentials
// carry no provider of their own, so the flag alone would choose which vendor
// API receives the stored token. Its messages hold names only, so they are safe
// to print.
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

// validateCreateProviderAgainstState protects a create rerun, which reads the
// flag as its provider and sends the server's stored token in preflight. State
// with no provider and no provider resources passes, because an earlier create
// failed before it reached a provider and this create records the provider. A
// recorded resource shows a provider owns it, so a flag cannot name that owner.
func (a *app) validateCreateProviderAgainstState(stPath string) error {
	expanded := config.Expand(stPath)
	exists, err := state.Exists(expanded)
	if err != nil || !exists {
		return err
	}
	st, err := state.Load(expanded)
	if err != nil {
		return err
	}
	if st.Compute.Provider == "" && st.Compute.ID == "" && len(st.Compute.ManagedResources) == 0 {
		return nil
	}
	return a.validateProviderFlag(st)
}
