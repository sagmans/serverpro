package importsync

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sagmans/serverpro/internal/compute"
	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/credentials"
	"github.com/sagmans/serverpro/internal/ownership"
	"github.com/sagmans/serverpro/internal/state"
)

const importOperationLockProbe = 50 * time.Millisecond

func TestImportAllWritesLocalArtifacts(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	stateRegistryPath = defaultRegistryPath
	candidate := Candidate{
		Provider:  "hetzner",
		ID:        "42",
		Name:      "demo-web",
		Namespace: "demo",
		Server:    "web",
		Location:  "fsn1",
		Size:      "cx23",
		Image:     "ubuntu-24.04",
		LabelsOK:  true,
		Record: compute.ServerRecord{
			Provider: "hetzner",
			ID:       "42",
			Name:     "demo-web",
			Labels:   ownership.ProviderLabels("demo", "web", nil),
			ProviderState: map[string]string{
				"access_policy_id": "9",
			},
		},
	}
	results, err := ImportAll(context.Background(), ImportOptions{
		Candidates:     []Candidate{candidate},
		ProviderToken:  "provider-token",
		TailscaleToken: "ts-token",
		AdminUser:      "ops",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Status != "imported" {
		t.Fatalf("results=%+v", results)
	}
	st, err := state.Load(config.ServerStatePath("demo", "web"))
	if err != nil {
		t.Fatal(err)
	}
	policyID, ok := compute.ManagedResourceID(st.Compute.ManagedResources, compute.ManagedResourceAccessPolicy)
	if st.Compute.ID != "42" || st.Compute.Provider != "hetzner" || st.Tailscale.Tailnet != config.Default().Access.Tailscale.Tailnet || !ok || policyID != "9" || len(st.Compute.ProviderState) != 0 {
		t.Fatalf("state=%+v", st)
	}
	cfg, err := config.Load(config.ServerConfigPath("demo", "web"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Compute.Name != "demo-web" || cfg.Namespace != "demo" || cfg.Admin.Username != "ops" {
		t.Fatalf("config=%+v", cfg)
	}
	creds, err := credentials.Load(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if creds.ServerProvider != "provider-token" || creds.Tailscale != "ts-token" {
		t.Fatalf("creds=%+v", creds)
	}
	reg, err := state.LoadRegistry(config.RegistryPath())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.Find("demo", "web"); !ok {
		t.Fatal("registry missing entry")
	}
	if _, err := os.Stat(filepath.Join(home, ".config/serverpro/namespaces/demo/servers/web/credentials.json")); err != nil {
		t.Fatal(err)
	}
}

func TestEnrichImportArtifactsAppliesOptionalProviderEvidence(t *testing.T) {
	candidate := importTestCandidate()
	opts := ImportOptions{
		Tailnet:        "example.ts.net",
		WithTailscale:  true,
		WithCloudflare: true,
		EnrichTailscale: func(context.Context, Candidate, config.Config) (state.TailscaleState, error) {
			return state.TailscaleState{NodeID: "node-1"}, nil
		},
		EnrichCloudflare: func(context.Context, Candidate, config.Config) (state.CloudflareState, error) {
			return state.CloudflareState{TunnelID: "tunnel-1", Name: "demo-web"}, nil
		},
	}
	cfg, st, err := enrichImportArtifacts(context.Background(), candidate, opts)
	if err != nil {
		t.Fatal(err)
	}
	if st.Tailscale.NodeID != "node-1" || st.Tailscale.Tailnet != opts.Tailnet || st.Cloudflare.TunnelID != "tunnel-1" {
		t.Fatalf("state=%+v", st)
	}
	if cfg.Network.Ingress != "cloudflare-tunnel" || !cfg.Cloudflare.Tunnel.Enabled || cfg.Cloudflare.Tunnel.Name != "demo-web" {
		t.Fatalf("config=%+v", cfg)
	}
}

func TestEnrichImportArtifactsStopsOnProviderFailure(t *testing.T) {
	providerErr := errors.New("provider enrichment failed")
	for _, test := range []struct {
		name string
		opts ImportOptions
	}{
		{name: "tailscale", opts: ImportOptions{WithTailscale: true, EnrichTailscale: func(context.Context, Candidate, config.Config) (state.TailscaleState, error) {
			return state.TailscaleState{}, providerErr
		}}},
		{name: "cloudflare", opts: ImportOptions{WithCloudflare: true, EnrichCloudflare: func(context.Context, Candidate, config.Config) (state.CloudflareState, error) {
			return state.CloudflareState{}, providerErr
		}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, _, err := enrichImportArtifacts(context.Background(), importTestCandidate(), test.opts); !errors.Is(err, providerErr) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestImportAllSkipsExistingWithoutForce(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	stateRegistryPath = defaultRegistryPath
	stPath := config.ServerStatePath("demo", "web")
	if err := state.Save(stPath, state.State{Namespace: "demo", Server: "web", Compute: state.ComputeState{ID: "1"}}); err != nil {
		t.Fatal(err)
	}
	results, err := ImportAll(context.Background(), ImportOptions{
		Candidates: []Candidate{{
			Provider: "hetzner", ID: "42", Name: "demo-web", Namespace: "demo", Server: "web", LabelsOK: true,
			Record: compute.ServerRecord{ID: "42", Name: "demo-web", Labels: ownership.ProviderLabels("demo", "web", nil)},
		}},
		ProviderToken: "token",
	})
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Status != "skipped" {
		t.Fatalf("results=%+v", results)
	}
}

func TestImportAllForcePreservesExistingIntentAndOwnershipEvidence(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	stateRegistryPath = defaultRegistryPath
	cfgPath := config.ServerConfigPath("demo", "web")
	cfg := config.ExampleServer("demo", "web")
	cfg.Admin.Username = "old-admin"
	cfg.Network.Egress.Mode = "open"
	cfg.Access.Tailscale.Tags = []string{"tag:serverpro-demo", "tag:serverpro-demo-web"}
	cfg.Cloudflare.AccountID = "old-account"
	cfg.Cloudflare.Tunnel.Enabled = true
	cfg.Cloudflare.Tunnel.CreateConnectorOnly = true
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	stPath := config.ServerStatePath("demo", "web")
	createdAt := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	if err := state.Save(stPath, state.State{
		Namespace: "demo",
		Server:    "web",
		Compute:   state.ComputeState{Provider: "vultr", ID: "old-id"},
		Tailscale: state.TailscaleState{
			Tailnet:         config.TokenDefaultTailnet,
			NodeID:          "old-node",
			AuthKeyID:       "auth-key",
			Tags:            []string{"tag:serverpro-demo", "tag:serverpro-demo-web"},
			PolicyTagOwners: []string{"tag:serverpro-demo-web"},
			PolicySSHRule:   true,
			PolicySSHTags:   []string{"tag:serverpro-demo", "tag:serverpro-demo-web"},
		},
		Cloudflare: state.CloudflareState{TunnelID: "tunnel-1", Name: "demo-web", Provenance: state.CloudflareTunnelCreated},
		Ingress:    []state.IngressState{{Type: "cloudflare-tunnel", Hostname: "app.example.com", Status: "pending"}},
		CreatedAt:  createdAt,
	}); err != nil {
		t.Fatal(err)
	}
	candidate := importTestCandidate()
	candidate.Provider = "vultr"
	candidate.ID = "new-id"
	candidate.Size = "vc2-2c-4gb"
	candidate.Record.ID = candidate.ID
	results, err := ImportAll(context.Background(), ImportOptions{
		Candidates:       []Candidate{candidate},
		ProviderToken:    "provider-token",
		TailscaleToken:   "tailscale-token",
		CloudflareToken:  "cloudflare-token",
		CloudflareAcctID: "new-account",
		AdminUser:        "new-admin",
		Tailnet:          "example.ts.net",
		WithTailscale:    true,
		WithCloudflare:   true,
		Force:            true,
		EnrichTailscale: func(context.Context, Candidate, config.Config) (state.TailscaleState, error) {
			return state.TailscaleState{NodeID: "new-node", Name: "demo-web.example.ts.net", Tags: []string{"tag:serverpro-demo", "tag:serverpro-demo-web"}}, nil
		},
		EnrichCloudflare: func(context.Context, Candidate, config.Config) (state.CloudflareState, error) {
			return state.CloudflareState{TunnelID: "tunnel-1", Name: "demo-web", Provenance: state.CloudflareTunnelImported}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Status != "imported" {
		t.Fatalf("results=%+v", results)
	}
	gotCfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if gotCfg.Admin.Username != "new-admin" || gotCfg.Compute.Size != candidate.Size || gotCfg.Access.Tailscale.Tailnet != "example.ts.net" || gotCfg.Cloudflare.AccountID != "new-account" {
		t.Fatalf("explicit refresh not applied: %+v", gotCfg)
	}
	if gotCfg.Network.Egress.Mode != "open" || !gotCfg.Cloudflare.Tunnel.CreateConnectorOnly || strings.Join(gotCfg.Access.Tailscale.Tags, ",") != "tag:serverpro-demo,tag:serverpro-demo-web" {
		t.Fatalf("operator intent not preserved: %+v", gotCfg)
	}
	gotState, err := state.Load(stPath)
	if err != nil {
		t.Fatal(err)
	}
	if gotState.Compute.ID != "new-id" || gotState.Tailscale.NodeID != "new-node" || gotState.Tailscale.Tailnet != "example.ts.net" {
		t.Fatalf("live identity not refreshed: %+v", gotState)
	}
	if gotState.Tailscale.AuthKeyID != "auth-key" || strings.Join(gotState.Tailscale.PolicyTagOwners, ",") != "tag:serverpro-demo-web" || !gotState.Tailscale.PolicySSHRule || strings.Join(gotState.Tailscale.PolicySSHTags, ",") != "tag:serverpro-demo,tag:serverpro-demo-web" {
		t.Fatalf("ownership evidence not preserved: %+v", gotState.Tailscale)
	}
	if gotState.Cloudflare.Provenance != state.CloudflareTunnelCreated || len(gotState.Ingress) != 1 || !gotState.CreatedAt.Equal(createdAt) {
		t.Fatalf("local state intent not preserved: %+v", gotState)
	}
}

func TestImportAllForceDoesNotTransferCloudflareOwnership(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	stateRegistryPath = defaultRegistryPath
	cfg := config.ExampleServer("demo", "web")
	cfg.Admin.Username = "ops"
	if err := config.Save(config.ServerConfigPath("demo", "web"), cfg); err != nil {
		t.Fatal(err)
	}
	if err := state.Save(config.ServerStatePath("demo", "web"), state.State{
		Namespace:  "demo",
		Server:     "web",
		Compute:    state.ComputeState{Provider: "vultr", ID: "old-id"},
		Cloudflare: state.CloudflareState{TunnelID: "old-tunnel", Name: "demo-web", Provenance: state.CloudflareTunnelCreated},
	}); err != nil {
		t.Fatal(err)
	}
	results, err := ImportAll(context.Background(), ImportOptions{
		Candidates:      []Candidate{importTestCandidate()},
		ProviderToken:   "provider-token",
		TailscaleToken:  "tailscale-token",
		CloudflareToken: "cloudflare-token",
		AdminUser:       "ops",
		WithCloudflare:  true,
		Force:           true,
		EnrichCloudflare: func(context.Context, Candidate, config.Config) (state.CloudflareState, error) {
			return state.CloudflareState{TunnelID: "new-tunnel", Name: "demo-web", Provenance: state.CloudflareTunnelImported}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Status != "imported" {
		t.Fatalf("results=%+v", results)
	}
	got, err := state.Load(config.ServerStatePath("demo", "web"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Cloudflare.TunnelID != "new-tunnel" || got.Cloudflare.Provenance != state.CloudflareTunnelImported {
		t.Fatalf("cloudflare state=%+v", got.Cloudflare)
	}
}

func TestImportAllForceRejectsMalformedExistingConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	stateRegistryPath = defaultRegistryPath
	cfgPath := config.ServerConfigPath("demo", "web")
	cfg := config.ExampleServer("demo", "web")
	cfg.Admin.Username = "ops"
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	malformed := []byte("namespace: [\n")
	if err := os.WriteFile(cfgPath, malformed, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := state.Save(config.ServerStatePath("demo", "web"), state.State{Namespace: "demo", Server: "web", Compute: state.ComputeState{ID: "old-id"}}); err != nil {
		t.Fatal(err)
	}
	results, err := ImportAll(context.Background(), ImportOptions{
		Candidates:    []Candidate{importTestCandidate()},
		ProviderToken: "provider-token",
		AdminUser:     "ops",
		Force:         true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Status != "failed" || !strings.Contains(results[0].Reason, "load existing config") {
		t.Fatalf("results=%+v", results)
	}
	body, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != string(malformed) {
		t.Fatalf("malformed config was replaced: %q", body)
	}
}

func TestImportAllForceRejectsConcurrentConfigEdit(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	stateRegistryPath = defaultRegistryPath
	cfgPath := config.ServerConfigPath("demo", "web")
	cfg := config.ExampleServer("demo", "web")
	cfg.Admin.Username = "ops"
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	if err := state.Save(config.ServerStatePath("demo", "web"), state.State{Namespace: "demo", Server: "web", Compute: state.ComputeState{ID: "old-id"}}); err != nil {
		t.Fatal(err)
	}
	replacement := cfg
	replacement.Network.Egress.Mode = "open"
	results, err := ImportAll(context.Background(), ImportOptions{
		Candidates:     []Candidate{importTestCandidate()},
		ProviderToken:  "provider-token",
		TailscaleToken: "tailscale-token",
		AdminUser:      "ops",
		Force:          true,
		beforeWrite: func(stage importWriteStage) error {
			if stage == importWriteConfig {
				return config.Save(cfgPath, replacement)
			}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Status != "failed" || !strings.Contains(results[0].Reason, config.ErrSourceChanged.Error()) {
		t.Fatalf("results=%+v", results)
	}
	got, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if got.Network.Egress.Mode != "open" {
		t.Fatalf("concurrent config was replaced: %+v", got.Network.Egress)
	}
}

func TestImportAllForceRetryPreservesExistingIntent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	stateRegistryPath = defaultRegistryPath
	cfgPath := config.ServerConfigPath("demo", "web")
	cfg := config.ExampleServer("demo", "web")
	cfg.Admin.Username = "ops"
	cfg.Network.Egress.Mode = "open"
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	stPath := config.ServerStatePath("demo", "web")
	if err := state.Save(stPath, state.State{
		Namespace: "demo",
		Server:    "web",
		Compute:   state.ComputeState{ID: "old-id"},
		Tailscale: state.TailscaleState{PolicyTagOwners: []string{"tag:serverpro-demo"}},
	}); err != nil {
		t.Fatal(err)
	}
	candidate := importTestCandidate()
	failed, err := ImportAll(context.Background(), ImportOptions{
		Candidates:     []Candidate{candidate},
		ProviderToken:  "provider-token",
		TailscaleToken: "tailscale-token",
		AdminUser:      "ops",
		Force:          true,
		beforeWrite: func(stage importWriteStage) error {
			if stage == importWriteCredentials {
				return errors.New("credential storage unavailable")
			}
			return nil
		},
	})
	if err != nil || len(failed) != 1 || failed[0].Status != "failed" {
		t.Fatalf("failed attempt=%+v err=%v", failed, err)
	}
	retried, err := ImportAll(context.Background(), ImportOptions{
		Candidates:     []Candidate{candidate},
		ProviderToken:  "provider-token",
		TailscaleToken: "tailscale-token",
		AdminUser:      "ops",
	})
	if err != nil || len(retried) != 1 || retried[0].Status != "imported" {
		t.Fatalf("retry=%+v err=%v", retried, err)
	}
	gotCfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	gotState, err := state.Load(stPath)
	if err != nil {
		t.Fatal(err)
	}
	if gotCfg.Network.Egress.Mode != "open" || strings.Join(gotState.Tailscale.PolicyTagOwners, ",") != "tag:serverpro-demo" {
		t.Fatalf("retry lost preserved intent: config=%+v state=%+v", gotCfg.Network.Egress, gotState.Tailscale)
	}
}

func TestImportAllReturnsStateStatErrors(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	stateRegistryPath = defaultRegistryPath
	results, err := ImportAll(context.Background(), ImportOptions{
		Candidates:    []Candidate{{Provider: "hetzner", ID: "1", Namespace: "invalid\x00namespace", Server: "web", LabelsOK: true}},
		ProviderToken: "provider-token",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Status != "failed" || !strings.Contains(results[0].Reason, "check local state") {
		t.Fatalf("stat error result = %+v", results)
	}
}

func TestImportAllWaitsForExistingServerOperation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	stateRegistryPath = defaultRegistryPath
	stPath := config.ServerStatePath("demo", "web")
	unlock, err := state.LockServerOperation(context.Background(), stPath)
	if err != nil {
		t.Fatal(err)
	}
	reachedWrite := make(chan struct{})
	done := make(chan []Result, 1)
	go func() {
		results, _ := ImportAll(context.Background(), ImportOptions{
			Candidates: []Candidate{importTestCandidate()}, ProviderToken: "token",
			beforeWrite: func(stage importWriteStage) error {
				if stage == importWriteMarker {
					close(reachedWrite)
				}
				return nil
			},
		})
		done <- results
	}()
	select {
	case <-reachedWrite:
		unlock()
		<-done
		t.Fatal("import reached local mutation while server operation lock was held")
	case <-time.After(importOperationLockProbe):
	}
	unlock()
	select {
	case results := <-done:
		if len(results) != 1 || results[0].Status != "imported" {
			t.Fatalf("import did not resume after lock release: %+v", results)
		}
	case <-time.After(time.Second):
		t.Fatal("import did not resume after server operation lock release")
	}
}

func TestImportAllWaitsForNamespaceOperation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	stateRegistryPath = defaultRegistryPath
	unlock, err := state.LockNamespaceOperationExclusive(context.Background(), defaultRegistryPath(), "demo")
	if err != nil {
		t.Fatal(err)
	}
	reachedWrite := make(chan struct{})
	done := make(chan []Result, 1)
	go func() {
		results, _ := ImportAll(context.Background(), ImportOptions{
			Candidates: []Candidate{importTestCandidate()}, ProviderToken: "token",
			beforeWrite: func(stage importWriteStage) error {
				if stage == importWriteMarker {
					close(reachedWrite)
				}
				return nil
			},
		})
		done <- results
	}()
	select {
	case <-reachedWrite:
		unlock()
		<-done
		t.Fatal("import reached local mutation while namespace operation lock was held")
	case <-time.After(importOperationLockProbe):
	}
	unlock()
	select {
	case results := <-done:
		if len(results) != 1 || results[0].Status != "imported" {
			t.Fatalf("import did not resume after namespace lock release: %+v", results)
		}
	case <-time.After(time.Second):
		t.Fatal("import did not resume after namespace operation lock release")
	}
}

func TestImportAllWaitsForMatchingTailnetPolicyOperation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	stateRegistryPath = defaultRegistryPath
	tailnet := "example.ts.net"
	unlock, err := state.LockTailnetPolicy(context.Background(), config.RegistryPath(), tailnet)
	if err != nil {
		t.Fatal(err)
	}
	reachedWrite := make(chan struct{})
	done := make(chan []Result, 1)
	go func() {
		results, _ := ImportAll(context.Background(), ImportOptions{
			Candidates: []Candidate{importTestCandidate()}, ProviderToken: "token", Tailnet: tailnet,
			beforeWrite: func(stage importWriteStage) error {
				if stage == importWriteMarker {
					close(reachedWrite)
				}
				return nil
			},
		})
		done <- results
	}()
	select {
	case <-reachedWrite:
		unlock()
		<-done
		t.Fatal("import reached local mutation while tailnet policy lock was held")
	case <-time.After(importOperationLockProbe):
	}
	unlock()
	select {
	case results := <-done:
		if len(results) != 1 || results[0].Status != "imported" {
			t.Fatalf("import did not resume after tailnet lock release: %+v", results)
		}
	case <-time.After(time.Second):
		t.Fatal("import did not resume after tailnet policy lock release")
	}
}

func TestImportAllDryRunDoesNotWrite(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	stateRegistryPath = defaultRegistryPath
	results, err := ImportAll(context.Background(), ImportOptions{
		Candidates: []Candidate{{
			Provider: "hetzner", ID: "42", Name: "demo-web", Namespace: "demo", Server: "web", LabelsOK: true,
			Record: compute.ServerRecord{ID: "42", Labels: ownership.ProviderLabels("demo", "web", nil)},
		}},
		ProviderToken: "token",
		DryRun:        true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Status != "planned" {
		t.Fatalf("results=%+v", results)
	}
	exists, err := state.Exists(config.ServerStatePath("demo", "web"))
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("dry-run wrote state")
	}
}

func TestImportAllRejectsDuplicateOwnership(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	_, err := ImportAll(context.Background(), ImportOptions{
		Candidates: []Candidate{
			{Provider: "hetzner", ID: "1", Namespace: "demo", Server: "web", LabelsOK: true},
			{Provider: "hetzner", ID: "2", Namespace: "demo", Server: "web", LabelsOK: true},
		},
		ProviderToken: "token",
	})
	if err == nil {
		t.Fatal("expected duplicate ownership error")
	}
}

func TestImportAllWriteStageFailuresAreAbsentOrMarkedResumable(t *testing.T) {
	stages := []importWriteStage{importWriteMarker, importWriteConfig, importWriteCredentials, importWriteState, importWriteRegistry}
	for _, stage := range stages {
		t.Run(string(stage), func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			stateRegistryPath = defaultRegistryPath
			stageErr := errors.New("injected stage failure")
			results, err := ImportAll(context.Background(), ImportOptions{
				Candidates:    []Candidate{importTestCandidate()},
				ProviderToken: "token",
				beforeWrite: func(got importWriteStage) error {
					if got == stage {
						return stageErr
					}
					return nil
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(results) != 1 || results[0].Status != "failed" || !strings.Contains(results[0].Reason, stageErr.Error()) {
				t.Fatalf("results=%+v", results)
			}
			stPath := config.ServerStatePath("demo", "web")
			markerExists := fileExists(importMarkerPath(stPath))
			artifactsExist := fileExists(config.ServerConfigPath("demo", "web")) ||
				fileExists(config.ServerCredentialsPath("demo", "web")) || fileExists(stPath) || fileExists(config.RegistryPath())
			if stage == importWriteMarker {
				if markerExists || artifactsExist {
					t.Fatalf("marker-stage failure published files: marker=%t artifacts=%t", markerExists, artifactsExist)
				}
			} else if !markerExists {
				t.Fatalf("stage %q left artifacts without recovery marker", stage)
			} else if markerBody, err := os.ReadFile(importMarkerPath(stPath)); err != nil {
				t.Fatal(err)
			} else if strings.Contains(string(markerBody), "token") {
				t.Fatalf("recovery marker exposed credential: %s", markerBody)
			}
		})
	}
}

func TestImportAllRetryAfterRegistryFailureCompletesWithoutForce(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	stateRegistryPath = defaultRegistryPath
	candidate := importTestCandidate()
	failed, err := ImportAll(context.Background(), ImportOptions{
		Candidates: []Candidate{candidate}, ProviderToken: "token",
		beforeWrite: func(stage importWriteStage) error {
			if stage == importWriteRegistry {
				return errors.New("registry unavailable")
			}
			return nil
		},
	})
	if err != nil || failed[0].Status != "failed" {
		t.Fatalf("failed attempt=%+v err=%v", failed, err)
	}
	stPath := config.ServerStatePath("demo", "web")
	if !fileExists(stPath) || !fileExists(importMarkerPath(stPath)) {
		t.Fatal("registry failure did not leave resumable state")
	}

	retried, err := ImportAll(context.Background(), ImportOptions{Candidates: []Candidate{candidate}, ProviderToken: "token"})
	if err != nil || retried[0].Status != "imported" {
		t.Fatalf("retry=%+v err=%v", retried, err)
	}
	reg, err := state.LoadRegistry(config.RegistryPath())
	if err != nil {
		t.Fatal(err)
	}
	if _, found := reg.Find("demo", "web"); !found || fileExists(importMarkerPath(stPath)) {
		t.Fatalf("retry did not commit registry and clear marker: found=%t marker=%t", found, fileExists(importMarkerPath(stPath)))
	}
}

func importTestCandidate() Candidate {
	return Candidate{
		Provider: "hetzner", ID: "42", Name: "demo-web", Namespace: "demo", Server: "web", LabelsOK: true,
		Record: compute.ServerRecord{ID: "42", Name: "demo-web", Labels: ownership.ProviderLabels("demo", "web", nil)},
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
