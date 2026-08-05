package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/assagman/serverpro/internal/config"
	"github.com/assagman/serverpro/internal/ingress"
	"github.com/assagman/serverpro/internal/state"
)

func TestIngressAddListRemoveCloudflareTunnel(t *testing.T) {
	createServerReadFixture(t)
	var out bytes.Buffer
	a := &app{stdout: &out, stderr: io.Discard, project: "demoapp"}
	add := a.ingressCmd()
	add.SetOut(&out)
	add.SetErr(io.Discard)
	add.SetArgs([]string{"add", "webapp", "--type", "cloudflare-tunnel", "--hostname", "app.example.com"})
	if err := add.Execute(); err != nil {
		t.Fatal(err)
	}
	var added ingressMutationRow
	if err := json.Unmarshal(out.Bytes(), &added); err != nil {
		t.Fatalf("add output is not JSON: %s", out.String())
	}
	if added.Status != "added" || added.Type != "cloudflare-tunnel" || added.Hostname != "app.example.com" {
		t.Fatalf("bad add output: %+v", added)
	}

	out.Reset()
	list := a.ingressCmd()
	list.SetOut(&out)
	list.SetErr(io.Discard)
	list.SetArgs([]string{"list", "webapp"})
	if err := list.Execute(); err != nil {
		t.Fatal(err)
	}
	var routes []state.IngressState
	if err := json.Unmarshal(out.Bytes(), &routes); err != nil {
		t.Fatalf("list output is not JSON: %s", out.String())
	}
	if len(routes) != 1 || routes[0].Type != "cloudflare-tunnel" || routes[0].Hostname != "app.example.com" || routes[0].Status != "pending" {
		t.Fatalf("bad list output: %+v", routes)
	}

	out.Reset()
	remove := a.ingressCmd()
	remove.SetOut(&out)
	remove.SetErr(io.Discard)
	remove.SetArgs([]string{"remove", "webapp", "--hostname", "app.example.com"})
	if err := remove.Execute(); err != nil {
		t.Fatal(err)
	}
	var removed ingressMutationRow
	if err := json.Unmarshal(out.Bytes(), &removed); err != nil {
		t.Fatalf("remove output is not JSON: %s", out.String())
	}
	if removed.Status != "removed" || removed.Hostname != "app.example.com" {
		t.Fatalf("bad remove output: %+v", removed)
	}
}

func TestIngressAddUsesRegisteredAdapter(t *testing.T) {
	createServerReadFixture(t)
	adapter := &recordingIngressAdapter{}

	var out bytes.Buffer
	a := &app{stdout: &out, stderr: io.Discard, project: "demoapp", ingressAdapters: map[ingress.Type]ingress.Adapter{testIngressType: adapter}}
	cmd := a.ingressCmd()
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"add", "webapp", "--type", string(testIngressType), "--hostname", "app.example.com"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if len(adapter.added) != 1 || adapter.added[0].Target != "demoapp-webapp" {
		t.Fatalf("adapter calls = %+v", adapter.added)
	}
	var row ingressMutationRow
	if err := json.Unmarshal(out.Bytes(), &row); err != nil {
		t.Fatalf("add output is not JSON: %s", out.String())
	}
	if row.Status != "added" || row.Type != string(testIngressType) || row.Hostname != "app.example.com" {
		t.Fatalf("bad add output: %+v", row)
	}
}

func TestIngressAddDryRunDoesNotMutateState(t *testing.T) {
	createServerReadFixture(t)
	var out bytes.Buffer
	cmd := New()
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--dry-run", "--non-interactive", "-n", "demoapp", "ingress", "add", "webapp", "--type", "cloudflare-tunnel", "--hostname", "app.example.com"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var row ingressMutationRow
	if err := json.Unmarshal(out.Bytes(), &row); err != nil {
		t.Fatalf("dry-run add output is not JSON: %s", out.String())
	}
	if row.Status != "planned" || !row.DryRun || row.Action != "add" || row.Hostname != "app.example.com" {
		t.Fatalf("bad dry-run add row: %+v", row)
	}
	_, st, err := (&app{project: "demoapp"}).loadServerReadState("webapp")
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Ingress) != 0 {
		t.Fatalf("dry-run add mutated ingress state: %+v", st.Ingress)
	}
}

func TestIngressRemoveDryRunDoesNotMutateState(t *testing.T) {
	createServerReadFixture(t)
	stPath := config.Expand(config.ServerStatePath("demoapp", "webapp"))
	st, err := state.Load(stPath)
	if err != nil {
		t.Fatal(err)
	}
	st.Ingress = []state.IngressState{{Type: "cloudflare-tunnel", Hostname: "app.example.com", Target: "demoapp-webapp", Status: "pending"}}
	if err := state.Save(stPath, st); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	cmd := New()
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--dry-run", "--non-interactive", "-n", "demoapp", "ingress", "remove", "webapp", "--hostname", "app.example.com"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var row ingressMutationRow
	if err := json.Unmarshal(out.Bytes(), &row); err != nil {
		t.Fatalf("dry-run remove output is not JSON: %s", out.String())
	}
	if row.Status != "planned" || !row.DryRun || row.Action != "remove" || row.Hostname != "app.example.com" {
		t.Fatalf("bad dry-run remove row: %+v", row)
	}
	_, stAfter, err := (&app{project: "demoapp"}).loadServerReadState("webapp")
	if err != nil {
		t.Fatal(err)
	}
	if len(stAfter.Ingress) != 1 || stAfter.Ingress[0].Hostname != "app.example.com" {
		t.Fatalf("dry-run remove mutated ingress state: %+v", stAfter.Ingress)
	}
}

func TestServerStatusShowsGenericIngressSummary(t *testing.T) {
	createServerReadFixture(t)
	var out bytes.Buffer
	a := &app{stdout: &out, stderr: io.Discard, project: "demoapp"}
	add := a.ingressCmd()
	add.SetOut(&out)
	add.SetErr(io.Discard)
	add.SetArgs([]string{"add", "webapp", "--type", "cloudflare-tunnel", "--hostname", "app.example.com"})
	if err := add.Execute(); err != nil {
		t.Fatal(err)
	}

	out.Reset()
	a = &app{stdout: &out, stderr: io.Discard, project: "demoapp", provider: "hetzner", providers: readProviderRegistry(t)}
	status := a.serverStatusCmd()
	status.SetOut(&out)
	status.SetErr(io.Discard)
	status.SetArgs([]string{"webapp"})
	if err := status.Execute(); err != nil {
		t.Fatal(err)
	}
	var row serverReadRow
	if err := json.Unmarshal(out.Bytes(), &row); err != nil {
		t.Fatalf("status output is not JSON: %s", out.String())
	}
	if row.Ingress != "cloudflare-tunnel:app.example.com" {
		t.Fatalf("status missing ingress summary: %+v", row)
	}
}

func TestIngressRemoveCallsAdapterBeforeStateMutation(t *testing.T) {
	createServerReadFixture(t)
	adapter := &recordingIngressAdapter{}

	var out bytes.Buffer
	a := &app{stdout: &out, stderr: io.Discard, project: "demoapp", ingressAdapters: map[ingress.Type]ingress.Adapter{testIngressType: adapter}}
	add := a.ingressCmd()
	add.SetOut(&out)
	add.SetErr(io.Discard)
	add.SetArgs([]string{"add", "webapp", "--type", string(testIngressType), "--hostname", "app.example.com"})
	if err := add.Execute(); err != nil {
		t.Fatal(err)
	}

	remove := a.ingressCmd()
	remove.SetOut(&out)
	remove.SetErr(io.Discard)
	remove.SetArgs([]string{"remove", "webapp", "--hostname", "app.example.com"})
	if err := remove.Execute(); err != nil {
		t.Fatal(err)
	}
	if len(adapter.removed) != 1 || adapter.removed[0].Hostname != "app.example.com" {
		t.Fatalf("adapter remove calls = %+v", adapter.removed)
	}
}

func TestIngressRemoveFailurePreservesState(t *testing.T) {
	createServerReadFixture(t)
	adapter := &recordingIngressAdapter{removeErr: errors.New("adapter failed")}

	a := &app{stdout: io.Discard, stderr: io.Discard, project: "demoapp", ingressAdapters: map[ingress.Type]ingress.Adapter{testIngressType: adapter}}
	add := a.ingressCmd()
	add.SetOut(io.Discard)
	add.SetErr(io.Discard)
	add.SetArgs([]string{"add", "webapp", "--type", string(testIngressType), "--hostname", "app.example.com"})
	if err := add.Execute(); err != nil {
		t.Fatal(err)
	}

	remove := a.ingressCmd()
	remove.SetOut(io.Discard)
	remove.SetErr(io.Discard)
	remove.SetArgs([]string{"remove", "webapp", "--hostname", "app.example.com"})
	err := remove.Execute()
	if err == nil || !strings.Contains(err.Error(), "adapter failed") {
		t.Fatalf("expected adapter error, got %v", err)
	}
	_, st, err := a.loadServerReadState("webapp")
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Ingress) != 1 || st.Ingress[0].Hostname != "app.example.com" {
		t.Fatalf("ingress state not preserved: %+v", st.Ingress)
	}
}

func TestIngressRemoveCleansAllMatchingHostnames(t *testing.T) {
	createServerReadFixture(t)
	adapter := &recordingIngressAdapter{}

	// Seed two ingress entries sharing a hostname directly into state. The add
	// path rejects duplicates, but state could hold them from manual edits or
	// older releases; remove must clean every match from its provider.
	stPath := config.Expand(config.ServerStatePath("demoapp", "webapp"))
	st, err := state.Load(stPath)
	if err != nil {
		t.Fatal(err)
	}
	st.Ingress = []state.IngressState{
		{Type: string(testIngressType), Hostname: "app.example.com", Target: "demoapp-webapp", Status: "configured"},
		{Type: string(testIngressType), Hostname: "app.example.com", Target: "demoapp-webapp", Status: "configured"},
	}
	if err := state.Save(stPath, st); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	a := &app{stdout: &out, stderr: io.Discard, project: "demoapp", ingressAdapters: map[ingress.Type]ingress.Adapter{testIngressType: adapter}}
	remove := a.ingressCmd()
	remove.SetOut(&out)
	remove.SetErr(io.Discard)
	remove.SetArgs([]string{"remove", "webapp", "--hostname", "app.example.com"})
	if err := remove.Execute(); err != nil {
		t.Fatal(err)
	}
	if len(adapter.removed) != 2 {
		t.Fatalf("expected 2 provider remove calls, got %d: %+v", len(adapter.removed), adapter.removed)
	}
	_, stAfter, err := a.loadServerReadState("webapp")
	if err != nil {
		t.Fatal(err)
	}
	if len(stAfter.Ingress) != 0 {
		t.Fatalf("expected no ingress left in state, got %+v", stAfter.Ingress)
	}
}

const testIngressType ingress.Type = "test-ingress"

type recordingIngressAdapter struct {
	added     []ingress.Route
	removed   []ingress.Route
	removeErr error
}

func (a *recordingIngressAdapter) Add(_ context.Context, route ingress.Route) (ingress.Route, error) {
	a.added = append(a.added, route)
	route.Type = testIngressType
	route.Status = "configured"
	return route, nil
}

func (a *recordingIngressAdapter) Remove(_ context.Context, route ingress.Route) error {
	a.removed = append(a.removed, route)
	return a.removeErr
}
