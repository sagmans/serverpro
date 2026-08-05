package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"maps"

	"github.com/sagmans/serverpro/internal/cloudinit"
	"github.com/sagmans/serverpro/internal/compute"
	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/ownership"
	"github.com/sagmans/serverpro/internal/state"
)

func renderProvisionUserData(cfg config.Config, tailscaleKey, adminPasswordHash string) (string, error) {
	return cloudinit.Render(cloudinit.Input{Config: cfg, TailscaleAuthKey: tailscaleKey, AdminPasswordHash: adminPasswordHash})
}

func ensureComputeServer(ctx context.Context, st *state.State, stPath string, cfg config.Config, account compute.Account, provider ComputeCreator, userData string, save provisionStateSaver) error {
	if st.Compute.ID != "" {
		return nil
	}
	record, diagnostics := provider.Create(ctx, compute.CreateServerRequest{Account: account, Intent: computeIntent(cfg), BootstrapData: userData, ManagedResources: append([]compute.ManagedResourceRef(nil), st.Compute.ManagedResources...), ProviderState: computeProviderState(*st)})
	if !diagnostics.Passed() {
		if record.ID != "" || len(record.ManagedResources) > 0 || len(record.ProviderState) > 0 {
			if err := applyComputeRecord(st, record); err != nil {
				return errors.Join(diagnostics.Err(), fmt.Errorf("partial compute checkpoint invalid: %w", err))
			}
			if err := save(stPath, *st); err != nil {
				return errors.Join(diagnostics.Err(), fmt.Errorf("partial compute checkpoint failed: %w", err))
			}
		}
		return diagnostics.Err()
	}
	if err := applyComputeRecord(st, record); err != nil {
		return fmt.Errorf("compute record invalid: %w", err)
	}
	return save(stPath, *st)
}

func computeIntent(cfg config.Config) compute.ServerIntent {
	return compute.ServerIntent{Namespace: cfg.Namespace, Server: cfg.Server, Name: cfg.Compute.Name, Location: cfg.Compute.Location, Size: cfg.Compute.Size, Image: cfg.Compute.Image, Labels: computeLabels(cfg)}
}

func computeLabels(cfg config.Config) map[string]string {
	return ownership.ProviderLabels(cfg.Namespace, cfg.Server, cfg.Compute.Labels)
}

func computeProviderState(st state.State) map[string]string {
	out := make(map[string]string, len(st.Compute.ProviderState))
	maps.Copy(out, st.Compute.ProviderState)
	return out
}

func applyComputeRecord(st *state.State, record compute.ServerRecord) error {
	resources, providerState, err := compute.CanonicalManagedResources(record.ManagedResources, record.ProviderState)
	if err != nil {
		return err
	}
	st.Namespace = record.Namespace
	st.Server = record.Server
	st.Compute = state.ComputeState{Provider: string(record.Provider), Namespace: record.Namespace, Server: record.Server, ID: record.ID, Name: record.Name, Location: record.Location, Size: record.Size, Image: record.Image, PublicIPv4: record.PublicIPv4, PublicIPv6: record.PublicIPv6, ManagedResources: resources, ProviderState: providerState}
	return nil
}
