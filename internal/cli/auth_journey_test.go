package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/doctor"
)

// authJourneySecret is a sentinel the journey searches for in every output
// and on disk, to prove a stored value never leaks after `auth set`.
const authJourneySecret = "journey-cloudflare-secret"

// runAuth drives the real command tree the way an agent does: arguments,
// optional piped stdin, JSON on stdout.
func runAuth(t *testing.T, stdin string, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	cmd := New()
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs(args)
	err := cmd.Execute()
	if strings.Contains(out.String(), authJourneySecret) {
		t.Fatalf("auth %v printed a stored secret: %s", args, out.String())
	}
	return out.String(), err
}

func mustAuth(t *testing.T, stdin string, args ...string) string {
	t.Helper()
	out, err := runAuth(t, stdin, args...)
	if err != nil {
		t.Fatalf("auth %v: %v\n%s", args, err, out)
	}
	return out
}

// TestAuthJourneyKeepsCredentialsSealed walks an operator from a fresh
// install through set, legacy migration, rotation, and restore, checking
// after each step that files stay sealed and readable.
func TestAuthJourneyKeepsCredentialsSealed(t *testing.T) {
	home := createTestHome(t)
	sealed := filepath.Join(home, ".config/serverpro/namespaces/demo/servers/web/credentials.json")
	requireAuthOutput(t, mustAuth(t, "", "auth", "init"), `"status": "created"`, "AGE-SECRET-KEY-PQ-1")
	original := strings.TrimSpace(mustAuth(t, "", "auth", "recovery-key"))
	mustAuth(t, authJourneySecret, "auth", "set", "web", "cloudflare_token", "-n", "demo")
	requireSealedFile(t, sealed)
	requireAuthOutput(t, mustAuth(t, "", "auth", "list", "-n", "demo"), `"cloudflare_token"`)
	if _, err := runAuth(t, "value", "auth", "set", "web", "nope", "-n", "demo"); err == nil {
		t.Fatal("unknown field accepted")
	}
	if _, err := runAuth(t, "value", "auth", "set", "web", "cloudflare_token"); err == nil {
		t.Fatal("set without a namespace accepted")
	}
	if _, err := runAuth(t, "", "auth", "set", "web", "cloudflare_token", "-n", "demo"); err == nil {
		t.Fatal("empty stdin accepted")
	}

	legacy := writeLegacyCredentials(t, home)
	if _, err := runAuth(t, "", "auth", "verify"); err == nil {
		t.Fatal("verify passed with a plaintext file")
	}
	if result := doctor.CheckCredentialStorage(); result.Status != doctor.Fail {
		t.Fatalf("doctor with plaintext = %+v", result)
	}
	requireAuthOutput(t, mustAuth(t, "", "--dry-run", "auth", "migrate"), legacy)
	requireAuthOutput(t, mustAuth(t, "", "auth", "migrate"), `"status": "migrated"`, "rotate them")
	requireSealedFile(t, legacy)
	mustAuth(t, "", "auth", "verify", "-n", "demo")
	if result := doctor.CheckCredentialStorage(); result.Status != doctor.Warn {
		t.Fatalf("doctor with a key file = %+v", result)
	}

	requireAuthOutput(t, mustAuth(t, "", "auth", "rotate-key"), `"status": "rotated"`)
	rotated := strings.TrimSpace(mustAuth(t, "", "auth", "recovery-key"))
	if rotated == original {
		t.Fatal("rotation kept the old key")
	}
	if _, err := runAuth(t, original, "auth", "restore", "--force"); err == nil {
		t.Fatal("restore accepted a key that opens no file")
	}
	requireAuthOutput(t, mustAuth(t, rotated, "auth", "restore"), `"status": "restored"`)
	requireAuthOutput(t, mustAuth(t, "", "auth", "status"), `"key_present": true`, `"encrypted_files": 2`)
	mustAuth(t, "", "auth", "remove", "web", "cloudflare_token", "-n", "demo")
	requireAuthOutput(t, mustAuth(t, "", "auth", "list", "-n", "demo"), `"server": "web"`)
}

func requireAuthOutput(t *testing.T, out string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
}

func requireSealedFile(t *testing.T, path string) {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(body, []byte("-----BEGIN AGE ENCRYPTED FILE-----")) || bytes.Contains(body, []byte(authJourneySecret)) {
		t.Fatalf("%s is not sealed:\n%s", path, body)
	}
}

// writeLegacyCredentials recreates a file from the plaintext release.
func writeLegacyCredentials(t *testing.T, home string) string {
	t.Helper()
	path := filepath.Join(home, ".config/serverpro/namespaces/demo/servers/legacy/credentials.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(map[string]string{"namespace": "demo", "server": "legacy", "tailscale_token": "legacy-token"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
