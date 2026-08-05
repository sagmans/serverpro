package importsync

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/compute"
	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/credentials"
	"github.com/sagmans/serverpro/internal/ownership"
	"github.com/sagmans/serverpro/internal/state"
)

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
	if st.Compute.ID != "42" || st.Compute.Provider != "hetzner" || st.Compute.ProviderState["access_policy_id"] != "9" {
		t.Fatalf("state=%+v", st)
	}
	cfg, err := config.Load(config.ServerConfigPath("demo", "web"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Compute.Name != "demo-web" || cfg.Project != "demo" || cfg.Admin.Username != "ops" {
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

func TestImportAllSkipsExistingWithoutForce(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	stateRegistryPath = defaultRegistryPath
	stPath := config.ServerStatePath("demo", "web")
	if err := state.Save(stPath, state.State{Namespace: "demo", Project: "demo", Server: "web", Compute: state.ComputeState{ID: "1"}}); err != nil {
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

func TestImportAllForceRejectsUnknownStateSchemaBeforeWrites(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	stateRegistryPath = defaultRegistryPath
	cfg := config.ExampleServer("demo", "web")
	cfgPath := config.ServerConfigPath("demo", "web")
	stPath := config.ServerStatePath("demo", "web")
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	if err := credentials.Save(cfg, credentials.Set{ServerProvider: "old-provider", Tailscale: "old-ts"}); err != nil {
		t.Fatal(err)
	}
	if err := state.Save(stPath, state.State{Namespace: "demo", Server: "web"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.Expand(stPath), []byte(`{"schema_version":99,"namespace":"demo","server":"web"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	reg := state.NewRegistry()
	reg.Upsert(state.RegistryEntry{Project: "demo", Server: "web", StatePath: stPath, ConfigPath: cfgPath, CredentialsPath: cfg.Credentials.JSONPath})
	if err := state.SaveRegistry(defaultRegistryPath(), reg); err != nil {
		t.Fatal(err)
	}
	paths := []string{config.Expand(cfgPath), config.Expand(cfg.Credentials.JSONPath), config.Expand(stPath), config.Expand(defaultRegistryPath())}
	before := make(map[string][]byte, len(paths))
	for _, path := range paths {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		before[path] = body
	}
	candidate := Candidate{Provider: "hetzner", ID: "42", Name: "demo-web", Namespace: "demo", Server: "web", Location: "fsn1", Size: "cx23", Image: "ubuntu-24.04", LabelsOK: true}
	results, err := ImportAll(context.Background(), ImportOptions{Candidates: []Candidate{candidate}, ProviderToken: "new-provider", Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Status != "failed" || !strings.Contains(results[0].Reason, "unsupported state schema version") {
		t.Fatalf("expected schema failure, got %+v", results[0])
	}
	for _, path := range paths {
		after, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(after, before[path]) {
			t.Fatalf("artifact changed before schema rejection: %s", path)
		}
	}
}

func TestImportAllRejectsMissingRecoveryMetadata(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*Candidate)
		want string
	}{
		{name: "provider id", edit: func(c *Candidate) { c.ID = "" }, want: "provider id"},
		{name: "name", edit: func(c *Candidate) { c.Name = "" }, want: "name"},
		{name: "location", edit: func(c *Candidate) { c.Location = "" }, want: "location"},
		{name: "size", edit: func(c *Candidate) { c.Size = "" }, want: "size"},
		{name: "image", edit: func(c *Candidate) { c.Image = "" }, want: "image"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			stateRegistryPath = defaultRegistryPath
			candidate := Candidate{Provider: "hetzner", ID: "42", Name: "demo-web", Namespace: "demo", Server: "web", Location: "fsn1", Size: "cx23", Image: "ubuntu-24.04", LabelsOK: true}
			tc.edit(&candidate)
			results, err := ImportAll(context.Background(), ImportOptions{Candidates: []Candidate{candidate}, ProviderToken: "token"})
			if err != nil {
				t.Fatal(err)
			}
			if results[0].Status != "failed" || !strings.Contains(results[0].Reason, tc.want) {
				t.Fatalf("expected missing %s failure, got %+v", tc.want, results[0])
			}
			if state.Exists(config.ServerStatePath("demo", "web")) {
				t.Fatal("invalid recovery metadata wrote state")
			}
		})
	}
}

func TestImportAllDryRunDoesNotWrite(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	stateRegistryPath = defaultRegistryPath
	results, err := ImportAll(context.Background(), ImportOptions{
		Candidates: []Candidate{{
			Provider: "hetzner", ID: "42", Name: "demo-web", Namespace: "demo", Server: "web", Location: "fsn1", Size: "cx23", Image: "ubuntu-24.04", LabelsOK: true,
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
	if state.Exists(config.ServerStatePath("demo", "web")) {
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
