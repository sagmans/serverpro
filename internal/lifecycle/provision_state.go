package lifecycle

import (
	"fmt"
	"time"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/state"
)

func initializeProvisionState(stPath string, cfg config.Config) (state.State, error) {
	st := state.State{Namespace: cfg.Namespace, Server: cfg.Server, Labels: cfg.Compute.Labels, Tailscale: state.TailscaleState{Tailnet: cfg.Access.Tailscale.Tailnet}}
	exists, err := state.Exists(stPath)
	if err != nil {
		return st, err
	}
	if !exists {
		return st, state.Save(stPath, st)
	}
	loaded, err := state.Load(stPath)
	if err != nil {
		return st, err
	}
	if err := state.ValidateTarget(state.Target{Namespace: cfg.Namespace, Server: cfg.Server, ComputeServerName: cfg.Compute.Name, CloudflareTunnelName: cfg.Cloudflare.Tunnel.Name}, loaded); err != nil {
		return st, err
	}
	if hasStableTailnetIdentity(loaded.Tailscale.Tailnet) && loaded.Tailscale.Tailnet != cfg.Access.Tailscale.Tailnet {
		return st, fmt.Errorf("state tailnet %q conflicts with config tailnet %q", loaded.Tailscale.Tailnet, cfg.Access.Tailscale.Tailnet)
	}
	if loaded.SchemaVersion != 0 && !loaded.CreatedAt.IsZero() && loaded.Tailscale.Tailnet == cfg.Access.Tailscale.Tailnet {
		return loaded, nil
	}
	// Reload under the state lock so migration cannot overwrite an ingress or
	// status update that lands after the initial target validation.
	if err := state.Update(stPath, func(current *state.State) error {
		if hasStableTailnetIdentity(current.Tailscale.Tailnet) && current.Tailscale.Tailnet != cfg.Access.Tailscale.Tailnet {
			return fmt.Errorf("state tailnet %q conflicts with config tailnet %q", current.Tailscale.Tailnet, cfg.Access.Tailscale.Tailnet)
		}
		current.Tailscale.Tailnet = cfg.Access.Tailscale.Tailnet
		return nil
	}); err != nil {
		return st, err
	}
	return state.Load(stPath)
}

func hasStableTailnetIdentity(tailnet string) bool {
	return tailnet != "" && tailnet != config.TokenDefaultTailnet
}

func saveProvisionState(path string, candidate state.State) error {
	return state.Update(path, func(current *state.State) error {
		// Lifecycle owns provider-resource checkpoints, while ingress and status
		// can be updated by concurrent read commands. Preserve those independent
		// fields instead of replacing the whole file with a stale snapshot.
		ingress := current.Ingress
		publicIPv4 := current.Compute.PublicIPv4
		publicIPv6 := current.Compute.PublicIPv6
		sameCompute := current.Compute.ID != "" && current.Compute.ID == candidate.Compute.ID
		*current = candidate
		current.Ingress = ingress
		if sameCompute {
			current.Compute.PublicIPv4 = publicIPv4
			current.Compute.PublicIPv6 = publicIPv6
		}
		return nil
	})
}

func completeProvision(stPath string, st *state.State, save provisionStateSaver, now time.Time) error {
	st.Validations = append(st.Validations, state.Validation{Time: now, Summary: "provision completed; run doctor for full matrix", Passed: true})
	return save(stPath, *st)
}
