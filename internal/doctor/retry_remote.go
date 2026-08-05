package doctor

import (
	"context"
	"strings"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/remote"
	"github.com/sagmans/serverpro/internal/state"
)

// RetryRemote refreshes only host-scoped evidence after sudo authentication;
// provider snapshots from the original run remain authoritative for that run.
func RetryRemote(ctx context.Context, cfg config.Config, st state.State, existing Report, runner remote.Runner, opt Options) Report {
	inventory := replaceRemoteInventory(existing.Inventory, remoteInventory(ctx, runner, cfg.Admin.Username, st.Tailscale.Name))
	results := replaceRemoteResults(existing.Results, remoteChecksWithOptions(ctx, cfg, runner, st.Tailscale.Name, opt))
	for i := range results {
		if results[i].Scope == "provider" && results[i].Name == "tailscale node" {
			results[i].Evidence = strings.ReplaceAll(results[i].Evidence, " ssh=ok", "")
		}
	}
	return Report{Inventory: inventory, Results: annotateTailscaleSSHStatus(results)}
}

func replaceRemoteInventory(current, replacement []InventoryItem) []InventoryItem {
	result := make([]InventoryItem, 0, len(current)+len(replacement))
	inserted := false
	for _, item := range current {
		if item.Scope == "remote" {
			if !inserted {
				result = append(result, replacement...)
				inserted = true
			}
			continue
		}
		result = append(result, item)
	}
	if !inserted {
		result = append(result, replacement...)
	}
	return result
}

func replaceRemoteResults(current, replacement []Result) []Result {
	result := make([]Result, 0, len(current)+len(replacement))
	inserted := false
	for _, item := range current {
		if item.Scope == "remote" {
			if !inserted {
				result = append(result, replacement...)
				inserted = true
			}
			continue
		}
		result = append(result, item)
	}
	if !inserted {
		result = append(result, replacement...)
	}
	return result
}
