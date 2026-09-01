//go:build serverpro_full_chain_e2e

package e2e_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	compiledDeleteCleanupFailureEnv      = "SERVERPRO_E2E_DELETE_CLEANUP_FAILURE"
	compiledDeleteCleanupFailPreflight   = "preflight"
	compiledDeleteExpectedProviderAssets = 2
)

func TestCompiledDeleteRejectsInvalidExternalCredentialBeforeComputeMutation(t *testing.T) {
	fixture := newProviderFixture(t)
	binary := buildE2EBinary(t)
	fakeBin := writeFakeTailscale(t)
	home := t.TempDir()
	namespace := "e2e-delete-preflight"
	writeCredentials(t, home, namespace)
	env := replaceEnv(journeyEnv(home, fakeBin, fixture.URL(), namespace), compiledDeleteCleanupFailureEnv, "")
	artifacts := newArtifactLog(t, "delete-preflight")

	create := runCommand(binary, env, "server", "create", testServer,
		"--namespace", namespace, "--provider", "hetzner",
		"--location", "fsn1", "--size", "cx23", "--image", "ubuntu-24.04",
		"--ingress", "none", "--non-interactive", "--yes")
	artifacts.record("create", create)
	requireSuccessJSON(t, create)

	failedDelete := runCommand(binary, replaceEnv(env, compiledDeleteCleanupFailureEnv, compiledDeleteCleanupFailPreflight),
		"server", "delete", testServer,
		"--namespace", namespace, "--provider", "hetzner", "--non-interactive", "--yes")
	artifacts.record("delete-preflight-failure", failedDelete)
	if failedDelete.err == nil || !strings.Contains(failedDelete.stderr, "401 Unauthorized") {
		t.Fatalf("invalid cleanup credential was not reported: err=%v stderr=%q", failedDelete.err, failedDelete.stderr)
	}
	if got := fixture.resourceCount("hetzner"); got != compiledDeleteExpectedProviderAssets {
		t.Fatalf("provider resources after preflight failure = %d, want %d", got, compiledDeleteExpectedProviderAssets)
	}
	statePath := filepath.Join(home, ".local", "state", "serverpro", "namespaces", namespace, "servers", testServer+".json")
	if _, err := os.Stat(statePath); err != nil {
		t.Fatalf("state missing after preflight failure: %v", err)
	}

	retry := runCommand(binary, env, "server", "delete", testServer,
		"--namespace", namespace, "--provider", "hetzner", "--non-interactive", "--yes")
	artifacts.record("delete-retry", retry)
	requireSuccessJSON(t, retry)
	if got := fixture.resourceCount("hetzner"); got != 0 {
		t.Fatalf("provider resources remain after retry: %d", got)
	}
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatalf("state remains after retry: %v", err)
	}
}

func replaceEnv(env []string, key, value string) []string {
	prefix := key + "="
	out := make([]string, 0, len(env)+1)
	for _, entry := range env {
		if !strings.HasPrefix(entry, prefix) {
			out = append(out, entry)
		}
	}
	return append(out, prefix+value)
}
