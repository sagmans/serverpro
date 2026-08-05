package lifecycle

import (
	"fmt"
	"time"

	"github.com/assagman/serverpro/internal/compute"
	"github.com/assagman/serverpro/internal/config"
	"github.com/assagman/serverpro/internal/state"
)

func initializeProvisionState(stPath string, cfg config.Config) (state.State, error) {
	st := state.State{Project: cfg.Project, Server: cfg.Server, Labels: cfg.Compute.Labels}
	if state.Exists(stPath) {
		loaded, err := state.Load(stPath)
		if err != nil {
			return st, err
		}
		if err := state.ValidateTarget(state.Target{Namespace: cfg.Project, Server: cfg.Server, ComputeServerName: cfg.Compute.Name, CloudflareTunnelName: cfg.Cloudflare.Tunnel.Name}, loaded); err != nil {
			return st, err
		}
		if loaded.SchemaVersion == 0 || loaded.CreatedAt.IsZero() {
			return loaded, state.Save(stPath, loaded)
		}
		return loaded, nil
	}
	return st, state.Save(stPath, st)
}

func checkpointProvisionIntent(stPath string, st *state.State, cfg config.Config, account compute.Account) error {
	if st.Compute.ID != "" {
		return nil
	}
	provider := string(account.Provider)
	if len(st.Compute.ProviderState) > 0 && st.Compute.Provider != "" && st.Compute.Provider != provider {
		return fmt.Errorf("compute provider mismatch: checkpoint uses %q, requested %q", st.Compute.Provider, provider)
	}
	intent := computeIntent(cfg)
	if st.Compute.Provider == provider && st.Compute.Namespace == intent.Namespace && st.Compute.Server == intent.Server && st.Compute.Name == intent.Name && st.Compute.Location == intent.Location && st.Compute.Size == intent.Size && st.Compute.Image == intent.Image {
		return nil
	}
	// Persisting intended identity before external writes makes partial creates
	// independently recoverable even when no compute resource was reached.
	st.Compute.Provider = provider
	st.Compute.Namespace = intent.Namespace
	st.Compute.Server = intent.Server
	st.Compute.Name = intent.Name
	st.Compute.Location = intent.Location
	st.Compute.Size = intent.Size
	st.Compute.Image = intent.Image
	return state.Save(stPath, *st)
}

func completeProvision(stPath string, st *state.State) error {
	st.Validations = append(st.Validations, state.Validation{Time: time.Now().UTC(), Summary: "provision completed; run doctor for full matrix", Passed: true})
	return state.Save(stPath, *st)
}
