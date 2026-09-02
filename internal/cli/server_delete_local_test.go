package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/privatefile"
	"github.com/sagmans/serverpro/internal/state"
)

const (
	testConfigLockSuffix          = ".lock"
	testStateLockSuffix           = ".lock"
	testServerOperationLockSuffix = ".operation.lock"
	testImportMarkerSuffix        = ".import.json"
	testUnrelatedCredentialFile   = "keep.txt"
	testLocalCleanupProbe         = 50 * time.Millisecond
)

func TestServerArtifactWorkflowWaitsForNamespaceDeletion(t *testing.T) {
	createServerReadFixture(t)
	st, err := state.Load(config.ServerStatePath("demoapp", "webapp"))
	if err != nil {
		t.Fatal(err)
	}
	unlockDelete, err := state.LockNamespaceOperationExclusive(context.Background(), config.RegistryPath(), st.NamespaceName())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), testLocalCleanupProbe)
	defer cancel()
	if _, err := lockServerArtifactWorkflow(ctx, st); err == nil {
		unlockDelete()
		t.Fatal("server artifact workflow bypassed namespace deletion")
	}
	unlockDelete()
}

func TestServerDeletePurgesCanonicalArtifacts(t *testing.T) {
	createServerReadFixture(t)
	statePath := config.ServerStatePath("demoapp", "webapp")
	markerPath := statePath + testImportMarkerSuffix
	if err := os.WriteFile(markerPath, []byte("{\"schema_version\":1}"), 0o600); err != nil {
		t.Fatal(err)
	}
	provider := &powerDeleteFakeProvider{}
	a := &app{stdout: io.Discard, stderr: io.Discard, project: "demoapp", provider: "hetzner", yes: true, providers: providerRegistryForPower(t, provider)}
	if err := a.runServerDelete(context.Background(), "webapp"); err != nil {
		t.Fatal(err)
	}
	for _, path := range canonicalDeleteTestPaths() {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("canonical artifact remains at %s: %v", path, err)
		}
	}
	credentialDir := filepath.Dir(config.ServerCredentialsPath("demoapp", "webapp"))
	if _, err := os.Lstat(credentialDir); !os.IsNotExist(err) {
		t.Fatalf("credential directory remains: %v", err)
	}
}

func TestServerDeletePreservesCustomConfig(t *testing.T) {
	createServerReadFixture(t)
	customPath := filepath.Join(t.TempDir(), "custom.yaml")
	customLockPath := customPath + testConfigLockSuffix
	if err := os.WriteFile(customPath, []byte("namespace: demoapp\nserver: webapp\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(customLockPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := state.UpdateRegistry(config.RegistryPath(), func(reg *state.Registry) error {
		entry, _ := reg.Find("demoapp", "webapp")
		entry.ConfigPath = customPath
		reg.Upsert(entry)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	provider := &powerDeleteFakeProvider{}
	a := &app{stdout: io.Discard, stderr: io.Discard, project: "demoapp", provider: "hetzner", yes: true, providers: providerRegistryForPower(t, provider)}
	if err := a.runServerDelete(context.Background(), "webapp"); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{customPath, customLockPath} {
		if _, err := os.Lstat(path); err != nil {
			t.Fatalf("custom artifact removed at %s: %v", path, err)
		}
	}
	if _, err := os.Lstat(config.ServerCredentialsPath("demoapp", "webapp")); !os.IsNotExist(err) {
		t.Fatalf("canonical credentials remain: %v", err)
	}
}

func TestServerDeletePreservesCustomState(t *testing.T) {
	createServerReadFixture(t)
	canonicalStatePath := config.ServerStatePath("demoapp", "webapp")
	st, err := state.Load(canonicalStatePath)
	if err != nil {
		t.Fatal(err)
	}
	customStatePath := filepath.Join(t.TempDir(), "custom.json")
	if err := state.Save(customStatePath, st); err != nil {
		t.Fatal(err)
	}
	if err := state.UpdateRegistry(config.RegistryPath(), func(reg *state.Registry) error {
		entry, _ := reg.Find("demoapp", "webapp")
		entry.StatePath = customStatePath
		reg.Upsert(entry)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	provider := &powerDeleteFakeProvider{}
	a := &app{statePath: customStatePath, stdout: io.Discard, stderr: io.Discard, project: "demoapp", provider: "hetzner", yes: true, providers: providerRegistryForPower(t, provider)}
	if err := a.runServerDelete(context.Background(), "webapp"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(customStatePath); err != nil {
		t.Fatalf("custom state removed: %v", err)
	}
	if _, err := os.Lstat(canonicalStatePath); !os.IsNotExist(err) {
		t.Fatalf("canonical state remains: %v", err)
	}
}

func TestServerDeleteKeepsNonEmptyCredentialDirectory(t *testing.T) {
	createServerReadFixture(t)
	credentialPath := config.ServerCredentialsPath("demoapp", "webapp")
	sentinelPath := filepath.Join(filepath.Dir(credentialPath), testUnrelatedCredentialFile)
	if err := os.WriteFile(sentinelPath, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	provider := &powerDeleteFakeProvider{}
	a := &app{stdout: io.Discard, stderr: io.Discard, project: "demoapp", provider: "hetzner", yes: true, providers: providerRegistryForPower(t, provider)}
	if err := a.runServerDelete(context.Background(), "webapp"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(credentialPath); !os.IsNotExist(err) {
		t.Fatalf("canonical credentials remain: %v", err)
	}
	if body, err := os.ReadFile(sentinelPath); err != nil || string(body) != "keep" {
		t.Fatalf("unrelated credential-directory file changed: body=%q err=%v", body, err)
	}
}

func TestServerDeleteRetainsRegistryOnCanonicalCleanupFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permission checks")
	}
	createServerReadFixture(t)
	configDir := filepath.Dir(config.ServerConfigPath("demoapp", "webapp"))
	if err := os.Chmod(configDir, 0o500); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(configDir, 0o700) }()
	provider := &powerDeleteFakeProvider{}
	a := &app{stdout: io.Discard, stderr: io.Discard, project: "demoapp", provider: "hetzner", yes: true, providers: providerRegistryForPower(t, provider)}
	if err := a.runServerDelete(context.Background(), "webapp"); err == nil {
		t.Fatal("delete reported success after canonical cleanup failure")
	}
	reg, err := state.LoadRegistry(config.RegistryPath())
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := reg.Find("demoapp", "webapp"); !exists {
		t.Fatal("registry authority removed after canonical cleanup failure")
	}
	for _, path := range []string{
		config.ServerConfigPath("demoapp", "webapp"),
		config.ServerCredentialsPath("demoapp", "webapp"),
		config.ServerStatePath("demoapp", "webapp"),
	} {
		if _, err := os.Lstat(path); err != nil {
			t.Fatalf("cleanup failure removed retry artifact %s: %v", path, err)
		}
	}
}

func TestServerDeleteWaitsForCanonicalArtifactUsers(t *testing.T) {
	createServerReadFixture(t)
	unlockArtifactUser, err := privatefile.LockSharedContext(context.Background(), config.LocalArtifactGuardPath())
	if err != nil {
		t.Fatal(err)
	}
	deleteReached := make(chan struct{})
	provider := &powerDeleteFakeProvider{deleteReached: deleteReached}
	a := &app{stdout: io.Discard, stderr: io.Discard, project: "demoapp", provider: "hetzner", yes: true, providers: providerRegistryForPower(t, provider)}
	done := make(chan error, 1)
	go func() { done <- a.runServerDelete(context.Background(), "webapp") }()
	select {
	case <-deleteReached:
	case <-time.After(time.Second):
		unlockArtifactUser()
		t.Fatal("delete did not reach remote cleanup")
	}
	select {
	case err := <-done:
		unlockArtifactUser()
		t.Fatalf("delete bypassed canonical artifact user: %v", err)
	case <-time.After(testLocalCleanupProbe):
	}
	unlockArtifactUser()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("delete did not resume after canonical artifact user released")
	}
	for _, path := range canonicalDeleteTestPaths() {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("canonical artifact remains at %s: %v", path, err)
		}
	}
}

func TestServerDeleteDryRunListsCanonicalLocalCleanup(t *testing.T) {
	createServerReadFixture(t)
	var out bytes.Buffer
	a := &app{stdout: &out, stderr: io.Discard, project: "demoapp", provider: "hetzner", dryRun: true}
	if err := a.runServerDelete(context.Background(), "webapp"); err != nil {
		t.Fatal(err)
	}
	var row struct {
		LocalCleanup []string `json:"local_cleanup"`
	}
	if err := json.Unmarshal(out.Bytes(), &row); err != nil {
		t.Fatal(err)
	}
	for _, want := range canonicalDeleteTestPaths() {
		found := false
		for _, got := range row.LocalCleanup {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("dry-run local cleanup missing %s: %v", want, row.LocalCleanup)
		}
	}
}

func TestServerDeleteConfirmationNamesCanonicalArtifacts(t *testing.T) {
	createServerReadFixture(t)
	provider := &powerDeleteFakeProvider{}
	var prompts bytes.Buffer
	a := &app{stdin: strings.NewReader("no\n"), stdout: io.Discard, stderr: &prompts, project: "demoapp", provider: "hetzner", providers: providerRegistryForPower(t, provider)}
	err := a.runServerDelete(context.Background(), "webapp")
	if err == nil {
		t.Fatal("delete confirmation unexpectedly accepted")
	}
	for _, want := range []string{"config", "credentials", "state", "synchronization"} {
		if !strings.Contains(prompts.String(), want) {
			t.Fatalf("confirmation missing %q: %s", want, prompts.String())
		}
	}
}

func canonicalDeleteTestPaths() []string {
	configPath := config.ServerConfigPath("demoapp", "webapp")
	credentialPath := config.ServerCredentialsPath("demoapp", "webapp")
	statePath := config.ServerStatePath("demoapp", "webapp")
	return []string{
		configPath,
		configPath + testConfigLockSuffix,
		credentialPath,
		statePath,
		statePath + testStateLockSuffix,
		statePath + testServerOperationLockSuffix,
		statePath + testImportMarkerSuffix,
	}
}
