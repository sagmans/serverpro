package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/sagmans/serverpro/internal/compute"
	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/credentials"
	"github.com/sagmans/serverpro/internal/doctor"
	"github.com/sagmans/serverpro/internal/passwordhash"
	"github.com/sagmans/serverpro/internal/state"
)

type createCatalogProvider struct{ cliFakeProvider }

func (createCatalogProvider) Catalog(context.Context, compute.CatalogQuery) (compute.Catalog, compute.Diagnostics) {
	return compute.Catalog{
		Locations: []compute.Location{{Name: "live-loc", City: "Live City", Country: "LC"}},
		Sizes:     []compute.Size{{Name: "live-size", Cores: 2, MemoryGB: 8, DiskGB: 80, Architecture: "x86"}},
		Images:    []compute.Image{{Name: "live-image", Architecture: "x86", OSFlavor: "ubuntu", OSVersion: "24.04"}},
	}, nil
}

func TestCreatePromptsUseLiveProviderCatalog(t *testing.T) {
	createTestHome(t)
	t.Setenv("SERVERPRO_SERVER_PROVIDER_TOKEN", "acct")
	registry := compute.NewRegistry()
	if err := registry.Register(createCatalogProvider{}); err != nil {
		t.Fatal(err)
	}
	seen := map[string][]choice{}
	a := &app{stdin: strings.NewReader("\n"), stdout: io.Discard, stderr: io.Discard, provider: "hetzner", providers: registry, selectChoice: func(label, def string, choices []choice) (string, bool, error) {
		seen[label] = choices
		return choices[0].Value, true, nil
	}}
	cfg := config.ExampleServer("demo", "web")
	if err := a.completeComputeConfig(&cfg, true); err != nil {
		t.Fatal(err)
	}
	if cfg.Compute.Location != "live-loc" || cfg.Compute.Size != "live-size" || cfg.Compute.Image != "live-image" {
		t.Fatalf("catalog choices not used: %+v", cfg.Compute)
	}
	for _, label := range []string{"compute location", "compute size", "compute image"} {
		if len(seen[label]) != 1 || !strings.HasPrefix(seen[label][0].Value, "live-") {
			t.Fatalf("%s choices = %+v", label, seen[label])
		}
	}
}

func TestCreateComputeAccountUsesServerCredentials(t *testing.T) {
	cfg := config.ExampleServer("demo", "web")
	a := &app{provider: "hetzner", providers: testProviderRegistry(t)}
	accountRef, err := a.computeAccountForConfig(cfg, credentials.Set{ServerProvider: "acct"})
	if err != nil {
		t.Fatal(err)
	}
	if accountRef.Name != "demo/web" || accountRef.Provider != "hetzner" || accountRef.Token != "acct" || accountRef.Scope != "demo/web" {
		t.Fatalf("bad compute credential: %+v", accountRef)
	}
}

func TestCreateDefaultNoIngressAllowsTailscaleOnlyServiceCredentials(t *testing.T) {
	dir := createTestHome(t)
	cfgPath := filepath.Join(dir, "serverpro.yaml")
	cfg := config.ExampleServer("demo", "web")
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	if err := credentials.Save(cfg, credentials.Set{Namespace: "demo", Server: "web", ServerProvider: "acct", Tailscale: "ts"}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEMO_WEB_SUDOPASS", "correct horse battery staple")
	a := &app{configPath: cfgPath, provider: "hetzner", nonInteractive: true, yes: true, stdout: io.Discard, services: serviceHooks{
		preflight: func(_ context.Context, got config.Config, creds credentials.Set) error {
			if got.Cloudflare.Tunnel.Enabled || got.Network.Ingress != "none" {
				t.Fatalf("test config should not use cloudflare: %+v", got.Cloudflare)
			}
			if creds.ServerProvider != "acct" || creds.Tailscale != "ts" || creds.Cloudflare != "" {
				t.Fatalf("create should accept tailscale-only credentials, got %+v", creds)
			}
			return nil
		},
		runProvision: func(context.Context, config.Config, string, compute.Account, credentials.Set, string, string) (state.State, error) {
			return state.State{}, errors.New("stop after credential check")
		},
	}}
	cmd := a.serverCmd()
	cmd.SetArgs([]string{"create", "web"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "stop after credential check") {
		t.Fatalf("expected sentinel error, got %v", err)
	}
}

func TestCreateCommandUsesServerScopedComputeToken(t *testing.T) {
	cfgPath := createTestConfig(t)
	accountToken := "acct"
	t.Setenv("DEMO_WEB_SUDOPASS", "correct horse battery staple")
	a := &app{configPath: cfgPath, provider: "hetzner", nonInteractive: true, yes: true, stdout: io.Discard, services: serviceHooks{
		preflight: func(_ context.Context, _ config.Config, creds credentials.Set) error {
			if len(creds.Missing()) != 0 {
				t.Fatalf("create did not load service credentials: %+v", creds)
			}
			return nil
		},
		runProvision: func(_ context.Context, _ config.Config, _ string, providerAccount compute.Account, creds credentials.Set, _, _ string) (state.State, error) {
			if providerAccount.Name != "demo/web" || providerAccount.Provider != "hetzner" || providerAccount.Token != accountToken {
				t.Fatalf("create did not pass server credential to provisioning: %+v", providerAccount)
			}
			return state.State{}, errors.New("stop after credential check")
		},
	}}
	cmd := a.serverCmd()
	cmd.SetArgs([]string{"create", "web"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "stop after credential check") {
		t.Fatalf("expected sentinel error, got %v", err)
	}
}

func TestCreateConfirmationUsesGenericResourceTerms(t *testing.T) {
	cfgPath := createTestConfig(t)
	var prompts bytes.Buffer
	a := &app{configPath: cfgPath, provider: "hetzner", stdin: strings.NewReader("n\n"), stdout: io.Discard, stderr: &prompts, services: serviceHooks{
		preflight: func(context.Context, config.Config, credentials.Set) error { return nil },
	}}
	cmd := a.serverCmd()
	cmd.SetArgs([]string{"create", "web"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "cancelled") {
		t.Fatalf("expected cancellation after confirmation prompt, got %v", err)
	}
	prompt := prompts.String()
	if strings.Contains(prompt, "Cloudflare") {
		t.Fatalf("confirmation leaked provider-specific terms:\n%s", prompt)
	}
	if !strings.Contains(prompt, "provider") || !strings.Contains(prompt, "ingress") {
		t.Fatalf("confirmation did not use generic terms:\n%s", prompt)
	}
}

func TestCreateCommandWritesSecretSafeProgressToStderr(t *testing.T) {
	cfgPath := createTestConfig(t)
	const sudoPassword = "correct horse battery staple"
	t.Setenv("DEMO_WEB_SUDOPASS", sudoPassword)
	var out bytes.Buffer
	var progress bytes.Buffer
	base := time.Date(2026, time.July, 23, 17, 0, 0, 0, time.UTC)
	tick := -1
	var adminPasswordHash string
	a := &app{configPath: cfgPath, provider: "hetzner", nonInteractive: true, yes: true, stdout: &out, stderr: &progress, now: func() time.Time {
		tick++
		return base.Add(time.Duration(tick) * time.Second)
	}, services: serviceHooks{
		preflight: func(context.Context, config.Config, credentials.Set) error { return nil },
		runProvision: func(_ context.Context, cfg config.Config, _ string, _ compute.Account, _ credentials.Set, _ string, hash string) (state.State, error) {
			adminPasswordHash = hash
			return state.State{Namespace: cfg.Namespace, Server: cfg.Server}, nil
		},
		doctorReport: func(context.Context, config.Config, state.State, credentials.Set, string, string) doctor.Report {
			return doctor.Report{Results: []doctor.Result{{Name: "smoke", Scope: "test", Status: doctor.Pass, Evidence: "ok"}}}
		},
	}}
	cmd := a.serverCreateCmd()
	cmd.SetArgs([]string{"web"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var report doctor.Report
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("create stdout is not one JSON report: %v\n%s", err, out.String())
	}
	got := progress.String()
	for _, want := range []string{
		"progress phase=preflight elapsed=1s attempt=1",
		"progress phase=provision elapsed=2s attempt=1",
		"progress phase=doctor elapsed=3s attempt=1",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("progress missing %q:\n%s", want, got)
		}
	}
	for _, secret := range []string{"acct", "ts", "cf", sudoPassword, adminPasswordHash} {
		if secret != "" && strings.Contains(got, secret) {
			t.Fatalf("progress leaked secret %q:\n%s", secret, got)
		}
	}
}

func TestCreateCommandWaitsForExistingServerOperation(t *testing.T) {
	cfgPath := createTestConfig(t)
	t.Setenv("DEMO_WEB_SUDOPASS", "correct horse battery staple")
	unlock, err := state.LockServerOperation(context.Background(), config.ServerStatePath("demo", "web"))
	if err != nil {
		t.Fatal(err)
	}
	a := &app{configPath: cfgPath, provider: "hetzner", nonInteractive: true, yes: true, stdout: io.Discard, stderr: io.Discard, create: createOverrides{ComputeName: "replacement-name"}, services: serviceHooks{
		preflight: func(context.Context, config.Config, credentials.Set) error { return nil },
		runProvision: func(context.Context, config.Config, string, compute.Account, credentials.Set, string, string) (state.State, error) {
			return state.State{}, errors.New("provision reached")
		},
	}}
	cmd := a.serverCreateCmd()
	cmd.SetArgs([]string{"web"})
	done := make(chan error, 1)
	go func() { done <- cmd.Execute() }()
	select {
	case err := <-done:
		unlock()
		t.Fatalf("create bypassed server operation lock: %v", err)
	case <-time.After(serverOperationLockProbe):
	}
	blockedConfig, err := config.LoadPartial(cfgPath)
	if err != nil {
		unlock()
		t.Fatal(err)
	}
	if blockedConfig.Compute.Name == "replacement-name" {
		unlock()
		t.Fatal("create published config before acquiring server operation lock")
	}
	unlock()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "provision reached") {
			t.Fatalf("create did not resume after lock release: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("create did not resume after server operation lock release")
	}
}

func TestCreateCommandWaitsForNamespaceOperation(t *testing.T) {
	cfgPath := createTestConfig(t)
	t.Setenv("DEMO_WEB_SUDOPASS", "correct horse battery staple")
	unlock, err := state.LockNamespaceOperationExclusive(context.Background(), config.RegistryPath(), "demo")
	if err != nil {
		t.Fatal(err)
	}
	reachedProvision := make(chan struct{})
	a := &app{configPath: cfgPath, provider: "hetzner", nonInteractive: true, yes: true, stdout: io.Discard, stderr: io.Discard, services: serviceHooks{
		preflight: func(context.Context, config.Config, credentials.Set) error { return nil },
		runProvision: func(context.Context, config.Config, string, compute.Account, credentials.Set, string, string) (state.State, error) {
			close(reachedProvision)
			return state.State{}, errors.New("provision reached")
		},
	}}
	cmd := a.serverCreateCmd()
	cmd.SetArgs([]string{"web"})
	done := make(chan error, 1)
	go func() { done <- cmd.Execute() }()
	select {
	case err := <-done:
		unlock()
		t.Fatalf("create bypassed namespace operation lock: %v", err)
	case <-time.After(serverOperationLockProbe):
	}
	select {
	case <-reachedProvision:
		unlock()
		t.Fatal("create provisioned while namespace deletion authority was held")
	default:
	}
	unlock()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "provision reached") {
			t.Fatalf("create did not resume after namespace lock release: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("create did not resume after namespace lock release")
	}
}

func TestCreateCommandRejectsRawSourceConfigDriftWhileWaitingForLock(t *testing.T) {
	cfgPath := createTestConfig(t)
	t.Setenv("DEMO_WEB_SUDOPASS", "correct horse battery staple")
	unlock, err := state.LockServerOperation(context.Background(), config.ServerStatePath("demo", "web"))
	if err != nil {
		t.Fatal(err)
	}
	provisioned := false
	a := &app{configPath: cfgPath, provider: "hetzner", nonInteractive: true, yes: true, stdout: io.Discard, stderr: io.Discard, create: createOverrides{ComputeName: "requested-name"}, services: serviceHooks{
		preflight: func(context.Context, config.Config, credentials.Set) error { return nil },
		runProvision: func(context.Context, config.Config, string, compute.Account, credentials.Set, string, string) (state.State, error) {
			provisioned = true
			return state.State{}, errors.New("provision reached")
		},
	}}
	cmd := a.serverCreateCmd()
	cmd.SetArgs([]string{"web"})
	done := make(chan error, 1)
	go func() { done <- cmd.Execute() }()
	select {
	case err := <-done:
		unlock()
		t.Fatalf("create bypassed operation lock: %v", err)
	case <-time.After(serverOperationLockProbe):
	}
	concurrent, err := config.LoadPartial(cfgPath)
	if err != nil {
		unlock()
		t.Fatal(err)
	}
	concurrent.Compute.Name = "concurrent-source-name"
	if err := config.Save(cfgPath, concurrent); err != nil {
		unlock()
		t.Fatal(err)
	}
	unlock()

	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "config changed") || provisioned {
			t.Fatalf("source drift err=%v provisioned=%t", err, provisioned)
		}
	case <-time.After(time.Second):
		t.Fatal("create did not finish after lock release")
	}
	preserved, err := config.LoadPartial(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if preserved.Compute.Name != "concurrent-source-name" {
		t.Fatalf("concurrent source edit overwritten: %q", preserved.Compute.Name)
	}
}

func TestLoadOrCreateConfigFromSourceUsesSnapshottedBytes(t *testing.T) {
	cfgPath := createTestConfig(t)
	source, err := snapshotConfigSource(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	replacement, err := config.LoadPartial(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	approvedLocation := replacement.Compute.Location
	replacement.Compute.Location = "replacement-location"
	if err := config.Save(cfgPath, replacement); err != nil {
		t.Fatal(err)
	}

	cfg, exists, err := (&app{configPath: cfgPath}).loadOrCreateConfigFromSource("demo", "web", source)
	if err != nil {
		t.Fatal(err)
	}
	if !exists || cfg.Compute.Location != approvedLocation {
		t.Fatalf("config was reparsed from changed source: exists=%t location=%q want=%q", exists, cfg.Compute.Location, approvedLocation)
	}
}

func TestCreateCommandWithTokenRelativeTailnetWaitsForExplicitPolicyOperation(t *testing.T) {
	cfgPath := createTestConfig(t)
	t.Setenv("DEMO_WEB_SUDOPASS", "correct horse battery staple")
	tailnet := "example.ts.net"
	unlock, err := state.LockTailnetPolicy(context.Background(), config.RegistryPath(), tailnet)
	if err != nil {
		t.Fatal(err)
	}
	reachedProvision := make(chan struct{})
	a := &app{configPath: cfgPath, provider: "hetzner", nonInteractive: true, yes: true, stdout: io.Discard, stderr: io.Discard, services: serviceHooks{
		preflight: func(context.Context, config.Config, credentials.Set) error { return nil },
		runProvision: func(context.Context, config.Config, string, compute.Account, credentials.Set, string, string) (state.State, error) {
			close(reachedProvision)
			return state.State{}, errors.New("provision reached")
		},
	}}
	cmd := a.serverCreateCmd()
	cmd.SetArgs([]string{"web"})
	done := make(chan error, 1)
	go func() { done <- cmd.Execute() }()
	select {
	case <-reachedProvision:
		unlock()
		<-done
		t.Fatal("create bypassed tailnet policy lock")
	case <-time.After(serverOperationLockProbe):
	}
	unlock()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "provision reached") {
			t.Fatalf("create did not resume after tailnet lock release: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("create did not resume after tailnet policy lock release")
	}
}

func TestCreateCommandRunsSetupProvisionDoctor(t *testing.T) {
	cfgPath := createTestConfig(t)
	t.Setenv("DEMO_WEB_SUDOPASS", "correct horse battery staple")
	var calls []string
	var out bytes.Buffer
	a := &app{configPath: cfgPath, provider: "hetzner", nonInteractive: true, yes: true, stdout: &out, services: serviceHooks{
		preflight: func(_ context.Context, got config.Config, creds credentials.Set) error {
			calls = append(calls, "preflight")
			if got.Namespace != "demo" || got.Server != "web" || creds.ServerProvider != "acct" || creds.Tailscale != "ts" {
				t.Fatalf("bad preflight args: cfg=%+v creds=%+v", got, creds)
			}
			return nil
		},
		runProvision: func(_ context.Context, got config.Config, stPath string, _ compute.Account, creds credentials.Set, sudoPassword, adminPasswordHash string) (state.State, error) {
			calls = append(calls, "runProvision")
			if sudoPassword != "correct horse battery staple" || !passwordhash.ValidSHA512(adminPasswordHash) {
				t.Fatalf("bad sudo material: password=%q hash=%q", sudoPassword, adminPasswordHash)
			}
			st := state.State{Namespace: got.Namespace, Server: got.Server, Compute: state.ComputeState{ID: "123", Name: got.Compute.Name}, Cloudflare: state.CloudflareState{Name: got.Cloudflare.Tunnel.Name}}
			if err := state.Save(stPath, st); err != nil {
				return state.State{}, err
			}
			return st, nil
		},
		doctorReport: func(_ context.Context, got config.Config, st state.State, creds credentials.Set, sudoPassword, adminPasswordHash string) doctor.Report {
			calls = append(calls, "doctorReport")
			if sudoPassword != "correct horse battery staple" || !passwordhash.ValidSHA512(adminPasswordHash) {
				t.Fatalf("bad sudo material for doctor: password=%q hash=%q", sudoPassword, adminPasswordHash)
			}
			if st.Compute.ID != "123" || creds.Cloudflare != "cf" {
				t.Fatalf("bad doctor args: cfg=%+v st=%+v creds=%+v", got, st, creds)
			}
			return doctor.Report{Results: []doctor.Result{{Name: "smoke", Scope: "test", Status: doctor.Pass, Evidence: "ok"}}}
		},
	}}
	cmd := a.serverCreateCmd()
	cmd.SetArgs([]string{"web"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(calls, []string{"preflight", "runProvision", "doctorReport"}) {
		t.Fatalf("calls = %v", calls)
	}
	reg, err := state.LoadRegistry(config.RegistryPath())
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := reg.Find("demo", "web")
	if !ok {
		t.Fatal("missing registry entry")
	}
	if entry.ConfigPath != cfgPath || entry.StatePath != config.ServerStatePath("demo", "web") {
		t.Fatalf("bad registry entry: %+v", entry)
	}
	var report doctor.Report
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("create output is not JSON: %s", out.String())
	}
	if len(report.Results) != 1 || report.Results[0].Name != "smoke" || report.Results[0].Status != doctor.Pass {
		t.Fatalf("bad output: %+v", report)
	}
}
