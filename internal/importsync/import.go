package importsync

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/assagman/serverpro/internal/config"
	"github.com/assagman/serverpro/internal/credentials"
	"github.com/assagman/serverpro/internal/ownership"
	"github.com/assagman/serverpro/internal/state"
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
	EnrichTailscale  func(context.Context, Candidate, config.Config) (state.TailscaleState, error)
	EnrichCloudflare func(context.Context, Candidate, config.Config) (state.CloudflareState, error)
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
	result := Result{
		Namespace:  candidate.Namespace,
		Server:     candidate.Server,
		Provider:   string(candidate.Provider),
		ProviderID: candidate.ID,
	}
	if !candidate.LabelsOK || candidate.Namespace == "" || candidate.Server == "" {
		result.Status = "failed"
		result.Reason = "missing serverpro ownership labels"
		return result
	}
	cfgPath := defaultServerConfigPath(candidate.Namespace, candidate.Server)
	stPath := defaultServerStatePath(candidate.Namespace, candidate.Server)
	result.ConfigPath = cfgPath
	result.StatePath = stPath
	if state.Exists(stPath) && !opts.Force {
		result.Status = "skipped"
		result.Reason = "local state exists; pass --force to overwrite"
		return result
	}
	if err := validateImportCandidate(candidate); err != nil {
		result.Status = "failed"
		result.Reason = err.Error()
		return result
	}
	if opts.Force && state.Exists(stPath) {
		if _, err := state.Load(stPath); err != nil {
			result.Status = "failed"
			result.Reason = err.Error()
			return result
		}
	}
	if _, err := state.LoadRegistry(defaultRegistryPath()); err != nil {
		result.Status = "failed"
		result.Reason = err.Error()
		return result
	}
	cfg := buildImportConfig(candidate, opts)
	st := buildImportState(candidate, cfg)
	if opts.WithTailscale && opts.EnrichTailscale != nil {
		ts, err := opts.EnrichTailscale(ctx, candidate, cfg)
		if err != nil {
			result.Status = "failed"
			result.Reason = err.Error()
			return result
		}
		st.Tailscale = ts
	}
	if opts.WithCloudflare && opts.EnrichCloudflare != nil {
		cf, err := opts.EnrichCloudflare(ctx, candidate, cfg)
		if err != nil {
			result.Status = "failed"
			result.Reason = err.Error()
			return result
		}
		st.Cloudflare = cf
		if cf.TunnelID != "" {
			cfg.Network.Ingress = "cloudflare-tunnel"
			cfg.Cloudflare.Tunnel.Enabled = true
			cfg.Cloudflare.Tunnel.Name = cf.Name
		}
	}
	if opts.DryRun {
		result.Status = "planned"
		return result
	}
	if err := writeImportArtifacts(cfg, st, cfgPath, stPath, opts); err != nil {
		result.Status = "failed"
		result.Reason = err.Error()
		return result
	}
	result.Status = "imported"
	return result
}

func buildImportConfig(candidate Candidate, opts ImportOptions) config.Config {
	cfg := config.ExampleServer(candidate.Namespace, candidate.Server)
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
	// WHY: never invent deploy on import; operator must supply real remote admin user.
	cfg.Admin.Username = opts.AdminUser
	if opts.Tailnet != "" {
		cfg.Access.Tailscale.Tailnet = opts.Tailnet
	}
	cfg.Compute.Labels = ownership.ConfigLabels(candidate.Namespace, candidate.Server, nil)
	if opts.WithCloudflare && opts.CloudflareAcctID != "" {
		cfg.Cloudflare.AccountID = opts.CloudflareAcctID
	}
	// Provider-only recovery still keeps mesh intent; tokens may be filled later for doctor/ssh.
	if opts.TailscaleToken == "" {
		cfg.Access.Tailscale.Enabled = false
	}
	if opts.CloudflareToken == "" {
		cfg.Cloudflare.Tunnel.Enabled = false
		if cfg.Network.Ingress == "cloudflare-tunnel" {
			cfg.Network.Ingress = "none"
		}
	}
	return cfg
}

func buildImportState(candidate Candidate, cfg config.Config) state.State {
	now := time.Now().UTC()
	labels := ownership.ConfigLabels(candidate.Namespace, candidate.Server, nil)
	return state.State{
		SchemaVersion: 1,
		Namespace:     candidate.Namespace,
		Project:       candidate.Namespace,
		Server:        candidate.Server,
		Compute: state.ComputeState{
			Provider:      string(candidate.Provider),
			Namespace:     candidate.Namespace,
			Server:        candidate.Server,
			ID:            candidate.ID,
			Name:          candidate.Name,
			Location:      candidate.Location,
			Size:          candidate.Size,
			Image:         candidate.Image,
			PublicIPv4:    candidate.PublicIPv4,
			ProviderState: copyProviderState(candidate.Record.ProviderState),
		},
		Labels:    labels,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func writeImportArtifacts(cfg config.Config, st state.State, cfgPath, stPath string, opts ImportOptions) error {
	if err := config.Save(cfgPath, cfg); err != nil {
		return err
	}
	creds := credentials.Set{
		Namespace:      cfg.Project,
		Project:        cfg.Project,
		Server:         cfg.Server,
		ServerProvider: opts.ProviderToken,
		Tailscale:      opts.TailscaleToken,
		Cloudflare:     opts.CloudflareToken,
	}
	if err := credentials.Save(cfg, creds); err != nil {
		return err
	}
	if err := state.Save(stPath, st); err != nil {
		return err
	}
	return state.UpdateRegistry(defaultRegistryPath(), func(reg *state.Registry) error {
		cfgAbs := config.Expand(cfgPath)
		if abs, err := filepath.Abs(cfgAbs); err == nil {
			cfgAbs = abs
		}
		reg.Upsert(state.RegistryEntry{
			Project:         cfg.Project,
			Server:          cfg.Server,
			StatePath:       stPath,
			ConfigPath:      cfgAbs,
			CredentialsPath: cfg.Credentials.JSONPath,
			ResourceNames: state.RegistryResourceNames{
				ComputeServer:    cfg.Compute.Name,
				CloudflareTunnel: cfg.Cloudflare.Tunnel.Name,
			},
			Labels: cfg.Compute.Labels,
		})
		return nil
	})
}

func validateImportCandidate(candidate Candidate) error {
	var missing []string
	for _, field := range []struct {
		name  string
		value string
	}{
		{name: "provider", value: string(candidate.Provider)},
		{name: "provider id", value: candidate.ID},
		{name: "name", value: candidate.Name},
		{name: "location", value: candidate.Location},
		{name: "size", value: candidate.Size},
		{name: "image", value: candidate.Image},
	} {
		if field.value == "" {
			missing = append(missing, field.name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("recovery metadata missing: %s", strings.Join(missing, ", "))
	}
	return nil
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
