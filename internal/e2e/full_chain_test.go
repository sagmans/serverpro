//go:build serverpro_full_chain_e2e

package e2e_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	testServer         = "web"
	testProviderToken  = "e2e-provider-token"
	testTailscaleToken = "e2e-tailscale-token"
	testSudoPassword   = "correct horse battery staple"
)

type providerCase struct {
	name     string
	location string
	size     string
	image    string
}

type commandResult struct {
	stdout string
	stderr string
	err    error
}

func TestCompiledFullChainJourneys(t *testing.T) {
	fixture := newProviderFixture(t)
	binary := buildE2EBinary(t)
	fakeBin := writeFakeTailscale(t)

	for _, provider := range []providerCase{
		{name: "hetzner", location: "fsn1", size: "cx23", image: "ubuntu-24.04"},
		{name: "vultr", location: "ewr", size: "vc2-1c-1gb", image: "1743"},
		{name: "digitalocean", location: "nyc3", size: "s-1vcpu-1gb", image: "ubuntu-24-04-x64"},
	} {
		t.Run(provider.name, func(t *testing.T) {
			t.Parallel()
			home := t.TempDir()
			namespace := "e2e-" + provider.name
			orchestrationAuditPath := filepath.Join(home, "orchestration-audit.log")
			writeCredentials(t, home, namespace)
			env := append(journeyEnv(home, fakeBin, fixture.URL(), namespace), "SERVERPRO_E2E_CLEANUP_AUDIT="+orchestrationAuditPath)
			artifacts := newArtifactLog(t, provider.name)

			create := runCommand(binary, env, "server", "create", testServer,
				"--namespace", namespace, "--provider", provider.name,
				"--location", provider.location, "--size", provider.size, "--image", provider.image,
				"--ingress", "none", "--non-interactive", "--yes")
			artifacts.record("create", create)
			requireSuccessJSON(t, create)

			status := runCommand(binary, env, "server", "status", testServer,
				"--namespace", namespace, "--provider", provider.name, "--non-interactive")
			artifacts.record("status", status)
			statusJSON := requireSuccessJSON(t, status)
			if statusJSON["provider"] != provider.name || statusJSON["server"] != testServer {
				t.Fatalf("unexpected status: %s", status.stdout)
			}

			doctor := runCommand(binary, env, "server", "doctor", testServer,
				"--namespace", namespace, "--provider", provider.name, "--non-interactive")
			artifacts.record("doctor", doctor)
			requireDoctorChecks(t, requireSuccessJSON(t, doctor))

			remove := runCommand(binary, env, "server", "delete", testServer,
				"--namespace", namespace, "--provider", provider.name, "--non-interactive", "--yes")
			artifacts.record("delete", remove)
			requireSuccessJSON(t, remove)

			if fixture.resourceCount(provider.name) != 0 {
				t.Fatalf("provider resources remain for %s: %d", provider.name, fixture.resourceCount(provider.name))
			}
			orchestrationAudit, err := os.ReadFile(orchestrationAuditPath)
			for _, want := range []string{"preflight-policy:checked", "provision-options:configured", "delete-device:e2e-device"} {
				if err != nil || !strings.Contains(string(orchestrationAudit), want) {
					t.Fatalf("production orchestration evidence %q missing: err=%v audit=%q", want, err, orchestrationAudit)
				}
			}
			statePath := filepath.Join(home, ".local", "state", "serverpro", "namespaces", namespace, "servers", testServer+".json")
			if _, err := os.Stat(statePath); !os.IsNotExist(err) {
				t.Fatalf("state still exists after delete: %v", err)
			}
		})
	}
}

func TestCompiledProviderOnlyImportRecovery(t *testing.T) {
	fixture := newProviderFixture(t)
	binary := buildE2EBinary(t)
	fakeBin := writeFakeTailscale(t)
	namespace := "e2e-import"
	createHome := t.TempDir()
	writeCredentials(t, createHome, namespace)
	createEnv := journeyEnv(createHome, fakeBin, fixture.URL(), namespace)
	artifacts := newArtifactLog(t, "import-recovery")

	create := runCommand(binary, createEnv, "server", "create", testServer,
		"--namespace", namespace, "--provider", "vultr",
		"--location", "ewr", "--size", "vc2-1c-1gb", "--image", "1743",
		"--ingress", "none", "--non-interactive", "--yes")
	artifacts.record("create", create)
	requireSuccessJSON(t, create)

	importHome := t.TempDir()
	importEnv := append(journeyEnv(importHome, fakeBin, fixture.URL(), namespace), "SERVERPRO_SERVER_PROVIDER_TOKEN="+testProviderToken)
	imported := runCommand(binary, importEnv, "server", "import", testServer,
		"--namespace", namespace, "--provider", "vultr", "--admin-user", "ops",
		"--non-interactive", "--yes")
	artifacts.record("import", imported)
	if imported.err != nil {
		t.Fatalf("provider-only import failed: %v\nstdout=%s\nstderr=%s", imported.err, imported.stdout, imported.stderr)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(imported.stdout), &rows); err != nil || len(rows) != 1 || rows[0]["status"] != "imported" {
		t.Fatalf("invalid import result: err=%v stdout=%s", err, imported.stdout)
	}

	doctor := runCommand(binary, importEnv, "server", "doctor", testServer,
		"--namespace", namespace, "--provider", "vultr", "--dry-run", "--non-interactive")
	artifacts.record("doctor-dry-run", doctor)
	doctorJSON := requireSuccessJSON(t, doctor)
	if doctorJSON["status"] != "planned" || doctorJSON["action"] != "doctor" {
		t.Fatalf("unexpected doctor dry-run: %s", doctor.stdout)
	}

	ssh := runCommand(binary, importEnv, "server", "ssh", testServer,
		"--namespace", namespace, "--provider", "vultr", "--dry-run", "--non-interactive")
	artifacts.record("ssh-recovery", ssh)
	wantRecovery := "serverpro server import web -n e2e-import -p vultr --force --with-tailscale"
	if ssh.err == nil || !strings.Contains(ssh.stderr, wantRecovery) {
		t.Fatalf("SSH recovery command missing: err=%v stderr=%q", ssh.err, ssh.stderr)
	}

	remove := runCommand(binary, createEnv, "server", "delete", testServer,
		"--namespace", namespace, "--provider", "vultr", "--non-interactive", "--yes")
	artifacts.record("delete", remove)
	requireSuccessJSON(t, remove)
}

func TestCompiledDigitalOceanLegacyImportRecovery(t *testing.T) {
	fixture := newProviderFixture(t)
	binary := buildE2EBinary(t)
	fakeBin := writeFakeTailscale(t)
	namespace := "e2e-legacy-import"
	createHome := t.TempDir()
	writeCredentials(t, createHome, namespace)
	createEnv := journeyEnv(createHome, fakeBin, fixture.URL(), namespace)
	artifacts := newArtifactLog(t, "digitalocean-legacy-import")

	create := runCommand(binary, createEnv, "server", "create", testServer,
		"--namespace", namespace, "--provider", "digitalocean",
		"--location", "nyc3", "--size", "s-1vcpu-1gb", "--image", "ubuntu-24-04-x64",
		"--ingress", "none", "--non-interactive", "--yes")
	artifacts.record("create", create)
	requireSuccessJSON(t, create)
	fixture.useLegacyDigitalOceanFirewall()

	importHome := t.TempDir()
	importEnv := append(journeyEnv(importHome, fakeBin, fixture.URL(), namespace), "SERVERPRO_SERVER_PROVIDER_TOKEN="+testProviderToken)
	imported := runCommand(binary, importEnv, "server", "import", testServer,
		"--namespace", namespace, "--provider", "digitalocean", "--admin-user", "ops",
		"--non-interactive", "--yes")
	artifacts.record("import", imported)
	if imported.err != nil {
		t.Fatalf("legacy DigitalOcean import failed: %v\nstdout=%s\nstderr=%s", imported.err, imported.stdout, imported.stderr)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(imported.stdout), &rows); err != nil || len(rows) != 1 || rows[0]["status"] != "imported" {
		t.Fatalf("invalid import result: err=%v stdout=%s", err, imported.stdout)
	}

	remove := runCommand(binary, createEnv, "server", "delete", testServer,
		"--namespace", namespace, "--provider", "digitalocean", "--non-interactive", "--yes")
	artifacts.record("delete", remove)
	requireSuccessJSON(t, remove)
}

func TestE2EBinaryRejectsNonLoopbackProviderAPI(t *testing.T) {
	binary := buildE2EBinary(t)
	result := runCommand(binary, append(os.Environ(), "SERVERPRO_E2E_API_URL=https://api.example.invalid"), "--help")
	if result.err == nil || !strings.Contains(result.stderr, "loopback") {
		t.Fatalf("non-loopback fixture accepted: err=%v stderr=%q", result.err, result.stderr)
	}
}

func TestCompiledCheckpointFailureKeepsRecoveryEvidenceAndCleanup(t *testing.T) {
	fixture := newProviderFixture(t)
	binary := buildE2EBinary(t)
	fakeBin := writeFakeTailscale(t)
	home := t.TempDir()
	namespace := "e2e-checkpoint"
	writeCredentials(t, home, namespace)
	marker := filepath.Join(home, "checkpoint-failed")
	env := append(journeyEnv(home, fakeBin, fixture.URL(), namespace), "SERVERPRO_E2E_FAIL_COMPLETE_CHECKPOINT="+marker)
	artifacts := newArtifactLog(t, "checkpoint")

	create := runCommand(binary, env, "server", "create", testServer,
		"--namespace", namespace, "--provider", "hetzner",
		"--location", "fsn1", "--size", "cx23", "--image", "ubuntu-24.04",
		"--ingress", "none", "--non-interactive", "--yes")
	artifacts.record("create-failure", create)
	if create.err == nil || !strings.Contains(create.stderr, "provision complete failed") ||
		!strings.Contains(create.stderr, "compute=42") || !strings.Contains(create.stderr, "access_policy=9") {
		t.Fatalf("checkpoint failure lacks typed recovery evidence: err=%v stderr=%q", create.err, create.stderr)
	}

	status := runCommand(binary, env, "server", "status", testServer,
		"--namespace", namespace, "--provider", "hetzner", "--non-interactive")
	artifacts.record("recovery-status", status)
	requireSuccessJSON(t, status)

	remove := runCommand(binary, env, "server", "delete", testServer,
		"--namespace", namespace, "--provider", "hetzner", "--non-interactive", "--yes")
	artifacts.record("recovery-delete", remove)
	requireSuccessJSON(t, remove)
	if fixture.resourceCount("hetzner") != 0 {
		t.Fatalf("checkpoint recovery left resources: %d", fixture.resourceCount("hetzner"))
	}
}

func buildE2EBinary(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(t.TempDir(), "serverpro-e2e")
	cmd := exec.Command("go", "build", "-tags", "serverpro_e2e", "-o", binary, "./cmd/serverpro-e2e")
	cmd.Dir = root
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build e2e binary: %v\n%s", err, output)
	}
	return binary
}

func writeFakeTailscale(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "tailscale")
	body := "#!/bin/sh\ncat >/dev/null\nprintf 'ok\\n'\n"
	if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func writeCredentials(t *testing.T, home, namespace string) {
	t.Helper()
	dir := filepath.Join(home, ".config", "serverpro", "namespaces", namespace, "servers", testServer)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(map[string]string{
		"namespace": namespace, "server": testServer,
		"server_provider_token": testProviderToken, "tailscale_token": testTailscaleToken,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "credentials.json"), body, 0o600); err != nil {
		t.Fatal(err)
	}
}

func journeyEnv(home, fakeBin, apiURL, namespace string) []string {
	return append(os.Environ(),
		"HOME="+home,
		"PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"SERVERPRO_E2E_API_URL="+apiURL,
		strings.ToUpper(strings.ReplaceAll(namespace, "-", "_X2D_"))+"_WEB_SUDOPASS="+testSudoPassword,
	)
}

func runCommand(binary string, env []string, args ...string) commandResult {
	cmd := exec.Command(binary, args...)
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return commandResult{stdout: stdout.String(), stderr: stderr.String(), err: err}
}

func requireSuccessJSON(t *testing.T, result commandResult) map[string]any {
	t.Helper()
	if result.err != nil {
		t.Fatalf("command failed: %v\nstdout=%s\nstderr=%s", result.err, result.stdout, result.stderr)
	}
	var value map[string]any
	if err := json.Unmarshal([]byte(result.stdout), &value); err != nil {
		t.Fatalf("stdout is not one JSON object: %v\n%s", err, result.stdout)
	}
	return value
}

func requireDoctorChecks(t *testing.T, report map[string]any) {
	t.Helper()
	results, ok := report["results"].([]any)
	if !ok {
		t.Fatalf("doctor results missing: %+v", report)
	}
	want := map[string]bool{
		"compute server":         false,
		"public ssh":             false,
		"tailscale node":         false,
		"sudo password required": false,
		"cloud-init":             false,
	}
	for _, raw := range results {
		result, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("invalid doctor result: %+v", raw)
		}
		if name, ok := result["name"].(string); ok {
			if _, expected := want[name]; expected {
				want[name] = true
			}
		}
	}
	for name, found := range want {
		if !found {
			t.Fatalf("production doctor check %q missing: %+v", name, report)
		}
	}
}

type artifactLog struct {
	t       *testing.T
	name    string
	entries []string
}

func newArtifactLog(t *testing.T, name string) *artifactLog {
	t.Helper()
	log := &artifactLog{t: t, name: name}
	t.Cleanup(log.flushOnFailure)
	return log
}

func (l *artifactLog) record(step string, result commandResult) {
	l.entries = append(l.entries, fmt.Sprintf("[%s]\nstdout:\n%s\nstderr:\n%s\nerror: %v\n", step, result.stdout, result.stderr, result.err))
}

func (l *artifactLog) flushOnFailure() {
	if !l.t.Failed() {
		return
	}
	dir := os.Getenv("SERVERPRO_E2E_ARTIFACT_DIR")
	if dir == "" {
		dir = l.t.TempDir()
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		l.t.Logf("create artifact directory: %v", err)
		return
	}
	body := strings.Join(l.entries, "\n")
	for _, secret := range []string{testProviderToken, testTailscaleToken, testSudoPassword} {
		body = strings.ReplaceAll(body, secret, "[REDACTED]")
	}
	if err := os.WriteFile(filepath.Join(dir, l.name+".log"), []byte(body), 0o600); err != nil {
		l.t.Logf("write artifacts: %v", err)
	}
}
