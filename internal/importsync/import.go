package importsync

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/sagmans/serverpro/internal/compute"
	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/ownership"
	"github.com/sagmans/serverpro/internal/state"
)

// ImportOptions controls local rewrite of config/state/credentials/registry.
type ImportOptions struct {
	Candidates       []Candidate
	ProviderToken    string
	TailscaleToken   string
	CloudflareToken  string
	CloudflareAcctID string
	AdminUser        string
	Tailnet          string
	WithTailscale    bool
	WithCloudflare   bool
	Force            bool
	DryRun           bool
	// Optional enrichers keep provider SDKs out of pure write path.
	EnrichTailscale      func(context.Context, Candidate, config.Config) (state.TailscaleState, error)
	EnrichCloudflare     func(context.Context, Candidate, config.Config) (state.CloudflareState, error)
	existingConfig       *config.Config
	existingConfigSource []byte
	existingState        *state.State
	checkConfigSource    bool
	beforeWrite          func(importWriteStage) error
}

// Result reports one import attempt.
type Result struct {
	Namespace  string `json:"namespace"`
	Server     string `json:"server"`
	Provider   string `json:"provider"`
	ProviderID string `json:"provider_id,omitempty"`
	Status     string `json:"status"`
	Reason     string `json:"reason,omitempty"`
	ConfigPath string `json:"config_path,omitempty"`
	StatePath  string `json:"state_path,omitempty"`
}

// importAttempt binds paths and result identity once so lock waits cannot
// redirect publication to newly resolved local targets.
type importAttempt struct {
	candidate  Candidate
	configPath string
	statePath  string
	result     Result
}

// ImportAll writes local SoT for each candidate; per-row failures do not abort the batch.
func ImportAll(ctx context.Context, opts ImportOptions) ([]Result, error) {
	if opts.ProviderToken == "" {
		return nil, fmt.Errorf("provider credential missing")
	}
	if len(opts.Candidates) == 0 {
		return nil, fmt.Errorf("no import candidates")
	}
	if err := validateCandidateKeys(opts.Candidates); err != nil {
		return nil, err
	}
	results := make([]Result, 0, len(opts.Candidates))
	for _, candidate := range opts.Candidates {
		results = append(results, importOne(ctx, candidate, opts))
	}
	return results, nil
}

func importOne(ctx context.Context, candidate Candidate, opts ImportOptions) Result {
	attempt, valid := newImportAttempt(candidate)
	if !valid {
		return importFailed(attempt.result, "missing serverpro ownership labels")
	}
	exists, err := state.Exists(attempt.statePath)
	if err != nil {
		return importFailed(attempt.result, "check local state: %v", err)
	}
	if opts.DryRun {
		return executeImportAttempt(ctx, attempt, opts, exists)
	}
	unlock, err := lockImportAttempt(ctx, attempt)
	if err != nil {
		return importFailed(attempt.result, "%v", err)
	}
	defer unlock()
	exists, err = state.Exists(attempt.statePath)
	if err != nil {
		return importFailed(attempt.result, "recheck local state: %v", err)
	}
	return executeImportAttempt(ctx, attempt, opts, exists)
}

func newImportAttempt(candidate Candidate) (importAttempt, bool) {
	result := Result{Namespace: candidate.Namespace, Server: candidate.Server, Provider: string(candidate.Provider), ProviderID: candidate.ID}
	attempt := importAttempt{candidate: candidate, result: result}
	if !candidate.LabelsOK || candidate.Namespace == "" || candidate.Server == "" {
		return attempt, false
	}
	attempt.configPath = defaultServerConfigPath(candidate.Namespace, candidate.Server)
	attempt.statePath = defaultServerStatePath(candidate.Namespace, candidate.Server)
	attempt.result.ConfigPath = attempt.configPath
	attempt.result.StatePath = attempt.statePath
	return attempt, true
}

func lockImportAttempt(ctx context.Context, attempt importAttempt) (func(), error) {
	unlock, err := state.LockServerWorkflow(ctx, defaultRegistryPath(), attempt.candidate.Namespace, attempt.statePath)
	if err != nil {
		return nil, fmt.Errorf("lock import workflow: %w", err)
	}
	return unlock, nil
}

func executeImportAttempt(ctx context.Context, attempt importAttempt, opts ImportOptions, exists bool) Result {
	resumable, err := importIsResumable(attempt.statePath, attempt.candidate)
	if err != nil {
		return importFailed(attempt.result, "check import transaction: %v", err)
	}
	if exists && !opts.Force && !resumable {
		return importStatus(attempt.result, "skipped", "local state exists; pass --force to overwrite")
	}
	// WHY: a matching retry must continue the original forced merge instead of
	// rebuilding defaults after a later transaction stage failed.
	if exists && (opts.Force || resumable) {
		existingState, loadErr := state.Load(attempt.statePath)
		if loadErr != nil {
			return importFailed(attempt.result, "load existing state: %v", loadErr)
		}
		opts.existingState = &existingState
		opts.checkConfigSource = true
		source, readErr := os.ReadFile(config.Expand(attempt.configPath))
		if readErr == nil {
			existingConfig, loadErr := config.LoadPartialBytes(source)
			if loadErr == nil && opts.Force {
				loadErr = existingConfig.Validate()
			}
			if loadErr != nil {
				return importFailed(attempt.result, "load existing config: %v", loadErr)
			}
			opts.existingConfig = &existingConfig
			opts.existingConfigSource = source
		} else if !errors.Is(readErr, os.ErrNotExist) {
			return importFailed(attempt.result, "load existing config: %v", readErr)
		}
	}
	cfg, st, err := enrichImportArtifacts(ctx, attempt.candidate, opts)
	if err != nil {
		return importFailed(attempt.result, "%v", err)
	}
	if opts.DryRun {
		return importStatus(attempt.result, "planned", "")
	}
	if err := publishImportArtifacts(ctx, cfg, st, attempt, opts); err != nil {
		return importFailed(attempt.result, "%v", err)
	}
	return importStatus(attempt.result, "imported", "")
}

func enrichImportArtifacts(ctx context.Context, candidate Candidate, opts ImportOptions) (config.Config, state.State, error) {
	cfg := buildImportConfig(candidate, opts)
	st := buildImportState(candidate, cfg, opts)
	if opts.WithTailscale && opts.EnrichTailscale != nil {
		tailscaleState, err := opts.EnrichTailscale(ctx, candidate, cfg)
		if err != nil {
			return cfg, st, err
		}
		if opts.existingState != nil {
			tailscaleState.AuthKeyID = opts.existingState.Tailscale.AuthKeyID
			tailscaleState.PolicyTagOwners = append([]string(nil), opts.existingState.Tailscale.PolicyTagOwners...)
			tailscaleState.PolicySSHRule = opts.existingState.Tailscale.PolicySSHRule
			tailscaleState.PolicySSHTags = append([]string(nil), opts.existingState.Tailscale.PolicySSHTags...)
		}
		tailscaleState.Tailnet = cfg.Access.Tailscale.Tailnet
		st.Tailscale = tailscaleState
		if len(tailscaleState.Tags) > 0 {
			cfg.Access.Tailscale.Tags = append([]string(nil), tailscaleState.Tags...)
		}
	}
	if opts.WithCloudflare && opts.EnrichCloudflare != nil {
		cloudflareState, err := opts.EnrichCloudflare(ctx, candidate, cfg)
		if err != nil {
			return cfg, st, err
		}
		if opts.existingState != nil && opts.existingState.Cloudflare.TunnelID == cloudflareState.TunnelID {
			switch opts.existingState.Cloudflare.Provenance {
			case state.CloudflareTunnelCreated, state.CloudflareTunnelAdopted:
				cloudflareState.Provenance = opts.existingState.Cloudflare.Provenance
			}
		}
		st.Cloudflare = cloudflareState
		if cloudflareState.TunnelID != "" {
			cfg.Network.Ingress = "cloudflare-tunnel"
			cfg.Cloudflare.Tunnel.Enabled = true
			cfg.Cloudflare.Tunnel.Name = cloudflareState.Name
		}
	}
	return cfg, st, nil
}

func publishImportArtifacts(ctx context.Context, cfg config.Config, st state.State, attempt importAttempt, opts ImportOptions) error {
	unlock, err := state.LockTailnetPolicy(ctx, defaultRegistryPath(), cfg.Access.Tailscale.Tailnet)
	if err != nil {
		return fmt.Errorf("lock tailnet policy: %v", err)
	}
	defer unlock()
	return writeImportArtifacts(cfg, st, attempt.configPath, attempt.statePath, opts)
}

func importFailed(result Result, format string, args ...any) Result {
	return importStatus(result, "failed", fmt.Sprintf(format, args...))
}

func importStatus(result Result, status, reason string) Result {
	result.Status = status
	result.Reason = reason
	return result
}

func buildImportConfig(candidate Candidate, opts ImportOptions) config.Config {
	cfg := config.ExampleServer(candidate.Namespace, candidate.Server)
	if opts.existingConfig != nil {
		cfg = *opts.existingConfig
		cfg.Namespace = candidate.Namespace
		cfg.Server = candidate.Server
	}
	if candidate.Name != "" {
		cfg.Compute.Name = candidate.Name
		cfg.Cloudflare.Tunnel.Name = candidate.Name
	}
	if candidate.Location != "" {
		cfg.Compute.Location = candidate.Location
	}
	if candidate.Size != "" {
		cfg.Compute.Size = candidate.Size
	}
	if candidate.Image != "" {
		cfg.Compute.Image = candidate.Image
	}
	// WHY: never invent deploy on fresh import or erase a known user during forced recovery.
	if opts.existingConfig == nil || opts.AdminUser != "" {
		cfg.Admin.Username = opts.AdminUser
	}
	if opts.Tailnet != "" {
		cfg.Access.Tailscale.Tailnet = opts.Tailnet
	}
	cfg.Compute.Labels = ownership.ConfigLabels(candidate.Namespace, candidate.Server, cfg.Compute.Labels)
	if opts.WithCloudflare && opts.CloudflareAcctID != "" {
		cfg.Cloudflare.AccountID = opts.CloudflareAcctID
	}
	// Provider-only recovery still keeps mesh intent; tokens may be filled later for doctor/ssh.
	if opts.existingConfig == nil && opts.TailscaleToken == "" {
		cfg.Access.Tailscale.Enabled = false
	}
	if opts.existingConfig == nil && opts.CloudflareToken == "" {
		cfg.Cloudflare.Tunnel.Enabled = false
		if cfg.Network.Ingress == "cloudflare-tunnel" {
			cfg.Network.Ingress = "none"
		}
	}
	return cfg
}

func buildImportState(candidate Candidate, cfg config.Config, opts ImportOptions) state.State {
	now := time.Now().UTC()
	labels := ownership.ConfigLabels(candidate.Namespace, candidate.Server, nil)
	st := state.State{
		SchemaVersion: 1,
		Namespace:     candidate.Namespace,
		Server:        candidate.Server,
		Compute: state.ComputeState{
			Provider:         string(candidate.Provider),
			Namespace:        candidate.Namespace,
			Server:           candidate.Server,
			ID:               candidate.ID,
			Name:             candidate.Name,
			Location:         candidate.Location,
			Size:             candidate.Size,
			Image:            candidate.Image,
			PublicIPv4:       candidate.PublicIPv4,
			ManagedResources: append([]compute.ManagedResourceRef(nil), candidate.Record.ManagedResources...),
			ProviderState:    copyProviderState(candidate.Record.ProviderState),
		},
		Tailscale: state.TailscaleState{Tailnet: cfg.Access.Tailscale.Tailnet},
		Labels:    labels,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if opts.existingState != nil {
		st.Tailscale = opts.existingState.Tailscale
		st.Tailscale.Tailnet = cfg.Access.Tailscale.Tailnet
		st.Cloudflare = opts.existingState.Cloudflare
		st.Ingress = append([]state.IngressState(nil), opts.existingState.Ingress...)
		st.CreatedAt = opts.existingState.CreatedAt
	}
	return st
}

func validateCandidateKeys(candidates []Candidate) error {
	seen := map[string]string{}
	for _, candidate := range candidates {
		if !candidate.LabelsOK || candidate.Namespace == "" || candidate.Server == "" {
			continue
		}
		key := candidate.Namespace + "/" + candidate.Server
		if prev, ok := seen[key]; ok && prev != candidate.ID {
			return fmt.Errorf("duplicate ownership labels for %s on provider ids %s and %s", key, prev, candidate.ID)
		}
		seen[key] = candidate.ID
	}
	return nil
}

func copyProviderState(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
