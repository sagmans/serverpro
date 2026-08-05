package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/provider/tailscale"
	"github.com/sagmans/serverpro/internal/state"
)

type fakeTailnetPolicyClient struct {
	plan             tailscale.ServerproPolicyReconcilePlan
	plannedProtected []string
	appliedProtected []string
	appliedPlan      tailscale.ServerproPolicyReconcilePlan
	applyCalls       int
	planCalled       chan struct{}
}

func (f *fakeTailnetPolicyClient) PlanServerproPolicyReconcile(_ context.Context, protected []string) (tailscale.ServerproPolicyReconcilePlan, error) {
	f.plannedProtected = append([]string(nil), protected...)
	if f.planCalled != nil {
		close(f.planCalled)
	}
	return f.plan, nil
}

func (f *fakeTailnetPolicyClient) ApplyServerproPolicyReconcile(_ context.Context, protected []string, approved tailscale.ServerproPolicyReconcilePlan) error {
	f.applyCalls++
	f.appliedProtected = append([]string(nil), protected...)
	f.appliedPlan = approved
	return nil
}

func TestTailnetReconcileDryRunProtectsOnlyMatchingTailnetState(t *testing.T) {
	createTestHome(t)
	stPath := config.ServerStatePath("demo", "web")
	if err := state.Save(stPath, state.State{
		Namespace: "demo", Server: "web", Tailscale: state.TailscaleState{Tailnet: "example.ts.net", Tags: []string{"tag:serverpro-registered"}},
	}); err != nil {
		t.Fatal(err)
	}
	otherPath := config.ServerStatePath("other", "api")
	if err := state.Save(otherPath, state.State{
		Namespace: "other", Server: "api", Tailscale: state.TailscaleState{Tailnet: "other.ts.net", Tags: []string{"tag:serverpro-other"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := state.UpdateRegistry(config.RegistryPath(), func(reg *state.Registry) error {
		reg.Upsert(state.RegistryEntry{Namespace: "demo", Server: "web", StatePath: stPath})
		reg.Upsert(state.RegistryEntry{Namespace: "other", Server: "api", StatePath: otherPath})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	client := &fakeTailnetPolicyClient{plan: tailscale.ServerproPolicyReconcilePlan{
		TagOwners: []string{"tag:serverpro-stale"},
		SSHRules:  []tailscale.SSHRule{{Action: "check", Dst: []string{"tag:serverpro-stale"}}},
	}}
	var out bytes.Buffer
	a := &app{stdout: &out, stderr: io.Discard, dryRun: true, nonInteractive: true, services: serviceHooks{
		tailnetPolicyClient: func(string, string) tailnetPolicyClient { return client },
	}}
	t.Setenv("SERVERPRO_TAILSCALE_TOKEN", "secret")
	cmd := a.tailnetReconcileCmd()
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--tailnet", "example.ts.net"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(client.plannedProtected, "tag:serverpro-registered") || slices.Contains(client.plannedProtected, "tag:serverpro-other") || client.applyCalls != 0 {
		t.Fatalf("protected=%v applyCalls=%d", client.plannedProtected, client.applyCalls)
	}
	var row tailnetReconcileRow
	if err := json.Unmarshal(out.Bytes(), &row); err != nil {
		t.Fatalf("output=%s err=%v", out.String(), err)
	}
	if row.Status != "planned" || !row.DryRun || len(row.TagOwners) != 1 || len(row.SSHRules) != 1 {
		t.Fatalf("row=%+v", row)
	}
}

func TestTailnetTokenUsesSupportedSources(t *testing.T) {
	t.Run("serverpro environment", func(t *testing.T) {
		t.Setenv("SERVERPRO_TAILSCALE_TOKEN", "primary")
		t.Setenv("TAILSCALE_API_TOKEN", "fallback")
		token, err := (&app{}).tailnetToken()
		if err != nil || token != "primary" {
			t.Fatalf("token=%q err=%v", token, err)
		}
	})
	t.Run("legacy environment", func(t *testing.T) {
		t.Setenv("SERVERPRO_TAILSCALE_TOKEN", "")
		t.Setenv("TAILSCALE_API_TOKEN", "fallback")
		token, err := (&app{}).tailnetToken()
		if err != nil || token != "fallback" {
			t.Fatalf("token=%q err=%v", token, err)
		}
	})
	t.Run("interactive prompt", func(t *testing.T) {
		t.Setenv("SERVERPRO_TAILSCALE_TOKEN", "")
		t.Setenv("TAILSCALE_API_TOKEN", "")
		token, err := (&app{stdin: strings.NewReader("prompted\n"), stderr: io.Discard}).tailnetToken()
		if err != nil || token != "prompted" {
			t.Fatalf("token=%q err=%v", token, err)
		}
	})
	t.Run("non-interactive missing", func(t *testing.T) {
		t.Setenv("SERVERPRO_TAILSCALE_TOKEN", "")
		t.Setenv("TAILSCALE_API_TOKEN", "")
		if _, err := (&app{nonInteractive: true}).tailnetToken(); err == nil || !strings.Contains(err.Error(), "required") {
			t.Fatalf("missing token error=%v", err)
		}
	})
}

func TestTailnetReconcileRequiresExplicitTailnet(t *testing.T) {
	createTestHome(t)
	client := &fakeTailnetPolicyClient{}
	a := &app{stdout: io.Discard, stderr: io.Discard, dryRun: true, nonInteractive: true, services: serviceHooks{
		tailnetPolicyClient: func(string, string) tailnetPolicyClient { return client },
	}}
	t.Setenv("SERVERPRO_TAILSCALE_TOKEN", "secret")
	cmd := a.tailnetReconcileCmd()
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "explicit") || client.plannedProtected != nil {
		t.Fatalf("token-relative tailnet accepted: err=%v protected=%v", err, client.plannedProtected)
	}
}

func TestTailnetReconcileCompletesWithoutApplyWhenPlanIsEmpty(t *testing.T) {
	createTestHome(t)
	client := &fakeTailnetPolicyClient{}
	var out bytes.Buffer
	a := &app{stdout: &out, stderr: io.Discard, nonInteractive: true, services: serviceHooks{
		tailnetPolicyClient: func(string, string) tailnetPolicyClient { return client },
	}}
	t.Setenv("SERVERPRO_TAILSCALE_TOKEN", "secret")
	cmd := a.tailnetReconcileCmd()
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--tailnet", "example.ts.net"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if client.applyCalls != 0 {
		t.Fatalf("apply calls=%d", client.applyCalls)
	}
	var row tailnetReconcileRow
	if err := json.Unmarshal(out.Bytes(), &row); err != nil || row.Status != "complete" || row.DryRun {
		t.Fatalf("row=%+v err=%v output=%s", row, err, out.String())
	}
}

func TestTailnetReconcileAppliesWithExplicitApproval(t *testing.T) {
	createTestHome(t)
	client := &fakeTailnetPolicyClient{plan: tailscale.ServerproPolicyReconcilePlan{TagOwners: []string{"tag:serverpro-stale"}}}
	var out bytes.Buffer
	a := &app{stdout: &out, stderr: io.Discard, yes: true, nonInteractive: true, services: serviceHooks{
		tailnetPolicyClient: func(string, string) tailnetPolicyClient { return client },
	}}
	t.Setenv("SERVERPRO_TAILSCALE_TOKEN", "secret")
	cmd := a.tailnetReconcileCmd()
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--tailnet", "example.ts.net"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if client.applyCalls != 1 || !reflect.DeepEqual(client.plannedProtected, client.appliedProtected) || !reflect.DeepEqual(client.plan, client.appliedPlan) {
		t.Fatalf("applyCalls=%d plannedProtected=%v appliedProtected=%v appliedPlan=%+v", client.applyCalls, client.plannedProtected, client.appliedProtected, client.appliedPlan)
	}
	var row tailnetReconcileRow
	if err := json.Unmarshal(out.Bytes(), &row); err != nil || row.Status != "complete" || len(row.TagOwners) != 1 {
		t.Fatalf("row=%+v err=%v output=%s", row, err, out.String())
	}
}

func TestTailnetReconcileInteractiveApplyWritesOneStdoutDocument(t *testing.T) {
	createTestHome(t)
	client := &fakeTailnetPolicyClient{plan: tailscale.ServerproPolicyReconcilePlan{TagOwners: []string{"tag:serverpro-stale"}}}
	var out bytes.Buffer
	var stderr bytes.Buffer
	a := &app{stdin: strings.NewReader("y\n"), stdout: &out, stderr: &stderr, services: serviceHooks{
		tailnetPolicyClient: func(string, string) tailnetPolicyClient { return client },
	}}
	t.Setenv("SERVERPRO_TAILSCALE_TOKEN", "secret")
	cmd := a.tailnetReconcileCmd()
	cmd.SetOut(&out)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"--tailnet", "example.ts.net"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var row tailnetReconcileRow
	if err := json.Unmarshal(out.Bytes(), &row); err != nil || row.Status != "complete" {
		t.Fatalf("stdout is not one completion document: row=%+v err=%v output=%s", row, err, out.String())
	}
	if !strings.Contains(stderr.String(), `"status": "planned"`) || !strings.Contains(stderr.String(), "remove tailnet policy") {
		t.Fatalf("stderr missing preview or prompt: %s", stderr.String())
	}
}

func TestTailnetReconcileFailsClosedOnUnknownTailnetIdentity(t *testing.T) {
	createTestHome(t)
	stPath := config.ServerStatePath("demo", "web")
	if err := state.Save(stPath, state.State{
		Namespace: "demo", Server: "web", Tailscale: state.TailscaleState{Tags: []string{"tag:serverpro-registered"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := state.UpdateRegistry(config.RegistryPath(), func(reg *state.Registry) error {
		reg.Upsert(state.RegistryEntry{Namespace: "demo", Server: "web", StatePath: stPath})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	client := &fakeTailnetPolicyClient{}
	a := &app{stdout: io.Discard, stderr: io.Discard, dryRun: true, nonInteractive: true, services: serviceHooks{
		tailnetPolicyClient: func(string, string) tailnetPolicyClient { return client },
	}}
	t.Setenv("SERVERPRO_TAILSCALE_TOKEN", "secret")
	cmd := a.tailnetReconcileCmd()
	cmd.SetArgs([]string{"--tailnet", "example.ts.net"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "tailnet identity") || client.plannedProtected != nil {
		t.Fatalf("unknown tailnet identity did not fail closed: err=%v protected=%v", err, client.plannedProtected)
	}
}

func TestTailnetReconcileWaitsForMatchingTailnetPolicyOperation(t *testing.T) {
	createTestHome(t)
	tailnet := "example.ts.net"
	unlock, err := state.LockTailnetPolicy(context.Background(), config.RegistryPath(), tailnet)
	if err != nil {
		t.Fatal(err)
	}
	client := &fakeTailnetPolicyClient{planCalled: make(chan struct{})}
	a := &app{stdout: io.Discard, stderr: io.Discard, dryRun: true, nonInteractive: true, services: serviceHooks{
		tailnetPolicyClient: func(string, string) tailnetPolicyClient { return client },
	}}
	t.Setenv("SERVERPRO_TAILSCALE_TOKEN", "secret")
	cmd := a.tailnetReconcileCmd()
	cmd.SetArgs([]string{"--tailnet", tailnet})
	done := make(chan error, 1)
	go func() { done <- cmd.Execute() }()
	select {
	case <-client.planCalled:
		unlock()
		<-done
		t.Fatal("reconcile planned while matching tailnet lock was held")
	case <-time.After(serverOperationLockProbe):
	}
	unlock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("reconcile did not resume after tailnet lock release: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("reconcile did not resume after tailnet lock release")
	}
}

func TestTailnetReconcileFailsClosedOnUnreadableRegisteredState(t *testing.T) {
	createTestHome(t)
	if err := state.UpdateRegistry(config.RegistryPath(), func(reg *state.Registry) error {
		reg.Upsert(state.RegistryEntry{Namespace: "demo", Server: "web", StatePath: config.ServerStatePath("demo", "missing")})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	client := &fakeTailnetPolicyClient{}
	a := &app{stdout: io.Discard, stderr: io.Discard, dryRun: true, nonInteractive: true, services: serviceHooks{
		tailnetPolicyClient: func(string, string) tailnetPolicyClient { return client },
	}}
	t.Setenv("SERVERPRO_TAILSCALE_TOKEN", "secret")
	cmd := a.tailnetReconcileCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--tailnet", "example.ts.net"})
	if err := cmd.Execute(); err == nil || client.plannedProtected != nil {
		t.Fatalf("expected fail-closed state error, err=%v protected=%v", err, client.plannedProtected)
	}
}
