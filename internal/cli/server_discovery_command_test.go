package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/compute"
	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/credentials"
	"github.com/sagmans/serverpro/internal/state"
)

func TestServerStatusAutoSelectsOnlyMatch(t *testing.T) {
	createDiscoveryFixture(t, []string{"demoapp"})
	var out bytes.Buffer
	a := &app{stdout: &out, stderr: io.Discard, providers: readProviderRegistry(t)}
	cmd := a.serverStatusCmd()
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"webapp"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var row serverReadRow
	if err := json.Unmarshal(out.Bytes(), &row); err != nil {
		t.Fatalf("auto-select output is not JSON:\n%s", out.String())
	}
	if row.Namespace != "demoapp" || row.Provider != "hetzner" {
		t.Fatalf("auto-select failed: %+v", row)
	}
}

func TestServerStatusNoMatchReportsTarget(t *testing.T) {
	createDiscoveryFixture(t, []string{"demoapp"})
	a := &app{stdout: io.Discard, stderr: io.Discard, providers: readProviderRegistry(t)}
	cmd := a.serverStatusCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"api"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "no servers matched \"api\"") {
		t.Fatalf("bad no-match error: %v", err)
	}
}

func TestServerStatusNonInteractiveAmbiguityGivesExactCommands(t *testing.T) {
	createDiscoveryFixture(t, []string{"demoapp", "sampleapp"})
	a := &app{stdout: io.Discard, stderr: io.Discard, nonInteractive: true, providers: readProviderRegistry(t)}
	cmd := a.serverStatusCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"webapp"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "multiple servers matched") || !strings.Contains(err.Error(), "serverpro server status webapp -n demoapp -p hetzner") || !strings.Contains(err.Error(), "serverpro server status webapp -n sampleapp -p hetzner") {
		t.Fatalf("bad ambiguity error: %v", err)
	}
}

func TestServerStatusUsesFZFSelectionWhenAvailable(t *testing.T) {
	createDiscoveryFixture(t, []string{"demoapp", "sampleapp"})
	var out bytes.Buffer
	a := &app{stdout: &out, stderr: io.Discard, providers: readProviderRegistry(t), selectServerMatch: func(options []string) (string, bool, error) {
		return options[1], true, nil
	}}
	cmd := a.serverStatusCmd()
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"webapp"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var row serverReadRow
	if err := json.Unmarshal(out.Bytes(), &row); err != nil {
		t.Fatalf("fzf output is not JSON:\n%s", out.String())
	}
	if row.Namespace != "sampleapp" {
		t.Fatalf("fzf selection not used: %+v", row)
	}
}

func TestServerStatusFallsBackToNumberedPrompt(t *testing.T) {
	createDiscoveryFixture(t, []string{"demoapp", "sampleapp"})
	var out, prompts bytes.Buffer
	a := &app{stdin: strings.NewReader("2\n"), stdout: &out, stderr: &prompts, providers: readProviderRegistry(t), selectServerMatch: func([]string) (string, bool, error) {
		return "", false, nil
	}}
	cmd := a.serverStatusCmd()
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"webapp"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"namespace": "sampleapp"`) || !strings.Contains(prompts.String(), "select server") {
		t.Fatalf("numbered fallback not used: stdout=%q stderr=%q", out.String(), prompts.String())
	}
}

func TestServerStatusAllShowsEveryMatch(t *testing.T) {
	createDiscoveryFixture(t, []string{"demoapp", "sampleapp"})
	var out bytes.Buffer
	a := &app{stdout: &out, stderr: io.Discard, all: true, providers: readProviderRegistry(t)}
	cmd := a.serverStatusCmd()
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"webapp"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "demoapp") || !strings.Contains(out.String(), "sampleapp") {
		t.Fatalf("--all did not show matches:\n%s", out.String())
	}
}

func TestServerStatusAllUsesEachMatchServerCredential(t *testing.T) {
	createDiscoveryCredentialFixture(t)
	provider := &credentialCheckingProvider{}
	var out bytes.Buffer
	a := &app{stdout: &out, stderr: io.Discard, all: true, providers: providerRegistryForPower(t, provider)}
	cmd := a.serverStatusCmd()
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"webapp"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if strings.Join(provider.scopes, ",") != "demoapp/webapp,sampleapp/webapp" {
		t.Fatalf("provider credential scopes = %v", provider.scopes)
	}
}

func createDiscoveryFixture(t *testing.T, namespaces []string) {
	t.Helper()
	createTestHome(t)
	reg := state.NewRegistry()
	for _, namespace := range namespaces {
		cfg := config.ExampleServer(namespace, "webapp")
		if err := credentials.Save(cfg, credentials.Set{Namespace: namespace, Server: "webapp", ServerProvider: "acct", Tailscale: "ts"}); err != nil {
			t.Fatal(err)
		}
		st := state.State{Namespace: namespace, Server: "webapp", Compute: state.ComputeState{Provider: "hetzner", Namespace: namespace, Server: "webapp", ID: "42", Name: namespace + "-webapp", Location: "fsn1", Size: "cpx22", Image: "ubuntu-24.04"}, Tailscale: state.TailscaleState{Name: namespace + "-webapp"}}
		stPath := config.ServerStatePath(namespace, "webapp")
		if err := state.Save(stPath, st); err != nil {
			t.Fatal(err)
		}
		reg.Upsert(state.RegistryEntry{Namespace: namespace, Server: "webapp", StatePath: stPath})
	}
	if err := state.SaveRegistry(config.RegistryPath(), reg); err != nil {
		t.Fatal(err)
	}
}

type credentialCheckingProvider struct {
	readFakeProvider
	scopes []string
}

func (p *credentialCheckingProvider) Status(_ context.Context, ref compute.ServerRef) (compute.ServerStatus, compute.Diagnostics) {
	p.scopes = append(p.scopes, ref.Account.Scope)
	if ref.Account.Token == "" || ref.Account.Scope != ref.Record.Namespace+"/"+ref.Record.Server {
		return compute.ServerStatus{}, compute.Diagnostics{{Status: compute.Fail, Message: "wrong credential for " + ref.Record.Namespace}}
	}
	return compute.ServerStatus{Power: "running", PublicIPv4: "203.0.113.11"}, nil
}

func createDiscoveryCredentialFixture(t *testing.T) {
	t.Helper()
	createTestHome(t)
	reg := state.NewRegistry()
	for _, namespace := range []string{"demoapp", "sampleapp"} {
		cfg := config.ExampleServer(namespace, "webapp")
		if err := credentials.Save(cfg, credentials.Set{Namespace: namespace, Server: "webapp", ServerProvider: namespace + "-token", Tailscale: "ts"}); err != nil {
			t.Fatal(err)
		}
		st := state.State{Namespace: namespace, Server: "webapp", Compute: state.ComputeState{Provider: "hetzner", Namespace: namespace, Server: "webapp", ID: "42", Name: namespace + "-webapp", Location: "fsn1", Size: "cpx22", Image: "ubuntu-24.04"}, Tailscale: state.TailscaleState{Name: namespace + "-webapp"}}
		stPath := config.ServerStatePath(namespace, "webapp")
		if err := state.Save(stPath, st); err != nil {
			t.Fatal(err)
		}
		reg.Upsert(state.RegistryEntry{Namespace: namespace, Server: "webapp", StatePath: stPath})
	}
	if err := state.SaveRegistry(config.RegistryPath(), reg); err != nil {
		t.Fatal(err)
	}
}
