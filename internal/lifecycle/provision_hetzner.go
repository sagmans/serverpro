package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"maps"

	"github.com/assagman/serverpro/internal/cloudinit"
	"github.com/assagman/serverpro/internal/compute"
	"github.com/assagman/serverpro/internal/config"
	"github.com/assagman/serverpro/internal/ownership"
	"github.com/assagman/serverpro/internal/state"
)

func renderProvisionUserData(cfg config.Config, tailscaleKey, adminPasswordHash string) (string, error) {
	return cloudinit.Render(cloudinit.Input{Config: cfg, TailscaleAuthKey: tailscaleKey, AdminPasswordHash: adminPasswordHash})
}

func ensureComputeServer(ctx context.Context, st *state.State, stPath string, cfg config.Config, account compute.Account, provider compute.Provider, userData string) error {
	if st.Compute.ID != "" {
		return nil
	}
	request := compute.CreateServerRequest{
		Account:       account,
		Intent:        computeIntent(cfg),
		BootstrapData: userData,
		ProviderState: computeProviderState(*st),
		CheckpointProviderState: func(providerState map[string]string) error {
			if st.Compute.ProviderState == nil {
				st.Compute.ProviderState = make(map[string]string)
			}
			maps.Copy(st.Compute.ProviderState, providerState)
			return state.Save(stPath, *st)
		},
	}
	record, diagnostics := provider.Create(ctx, request)
	if !diagnostics.Passed() {
		if record.ID != "" || len(record.ProviderState) > 0 {
			applyComputeRecord(st, record)
			if err := state.Save(stPath, *st); err != nil {
				return errors.Join(diagnostics.Err(), fmt.Errorf("partial compute checkpoint failed: %w", err))
			}
		}
		return diagnostics.Err()
	}
	applyComputeRecord(st, record)
	return state.Save(stPath, *st)
}

func computeIntent(cfg config.Config) compute.ServerIntent {
	return compute.ServerIntent{Namespace: cfg.Project, Server: cfg.Server, Name: cfg.Compute.Name, Location: cfg.Compute.Location, Size: cfg.Compute.Size, Image: cfg.Compute.Image, Labels: computeLabels(cfg)}
}

func computeLabels(cfg config.Config) map[string]string {
	return ownership.ProviderLabels(cfg.Project, cfg.Server, cfg.Compute.Labels)
}

func computeProviderState(st state.State) map[string]string {
	out := make(map[string]string, len(st.Compute.ProviderState))
	maps.Copy(out, st.Compute.ProviderState)
	return out
}

func applyComputeRecord(st *state.State, record compute.ServerRecord) {
	st.Namespace = record.Namespace
	st.Project = record.Namespace
	st.Server = record.Server
	st.Compute = state.ComputeState{Provider: string(record.Provider), Namespace: record.Namespace, Server: record.Server, ID: record.ID, Name: record.Name, Location: record.Location, Size: record.Size, Image: record.Image, PublicIPv4: record.PublicIPv4, PublicIPv6: record.PublicIPv6, ProviderState: record.ProviderState}
}
