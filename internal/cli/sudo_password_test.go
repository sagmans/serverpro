package cli

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/credentials"
	"github.com/sagmans/serverpro/internal/redact"
)

func TestSudoPasswordEnvNameUsesCollisionSafeEncoding(t *testing.T) {
	name, err := sudoPasswordEnvName("example.com", "webapp")
	if err != nil {
		t.Fatal(err)
	}
	if name != "EXAMPLE_X2E_COM_WEBAPP_SUDOPASS" {
		t.Fatalf("env name = %q", name)
	}

	left, err := sudoPasswordEnvName("a.b", "web-app")
	if err != nil {
		t.Fatal(err)
	}
	right, err := sudoPasswordEnvName("a-b", "web_app")
	if err != nil {
		t.Fatal(err)
	}
	if left == right {
		t.Fatalf("env names collided: %q", left)
	}
	encoded, err := sudoPasswordEnvName("a.b", "web")
	if err != nil {
		t.Fatal(err)
	}
	literalMarker, err := sudoPasswordEnvName("a_X2E_b", "web")
	if err != nil {
		t.Fatal(err)
	}
	if encoded == literalMarker {
		t.Fatalf("encoded unsafe char collided with literal marker: %q", encoded)
	}
}

func TestResolveSudoPasswordUsesEnvWithoutPrompt(t *testing.T) {
	createTestHome(t)
	t.Setenv("EXAMPLE_X2E_COM_WEBAPP_SUDOPASS", "correct horse battery staple")
	cfg := config.ExampleServer("example.com", "webapp")
	a := &app{nonInteractive: true, stdin: strings.NewReader("should-not-read\n"), stdout: io.Discard}
	got, err := a.resolveSudoPassword(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got != "correct horse battery staple" {
		t.Fatalf("password = %q", got)
	}
}

func TestResolveSudoPasswordPromptsOnceAndCaches(t *testing.T) {
	createTestHome(t)
	cfg := config.ExampleServer("demo", "web")
	a := &app{stdin: strings.NewReader("correct horse battery staple\nsecond password should not read\n"), stdout: io.Discard}
	first, err := a.resolveSudoPassword(cfg)
	if err != nil {
		t.Fatal(err)
	}
	second, err := a.resolveSudoPassword(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if first != "correct horse battery staple" || second != first {
		t.Fatalf("cached passwords = %q then %q", first, second)
	}
}

func TestResolveSudoPasswordRejectsMissingNonInteractiveEnv(t *testing.T) {
	createTestHome(t)
	cfg := config.ExampleServer("demo", "web")
	a := &app{nonInteractive: true, stdin: strings.NewReader("ignored\n"), stdout: io.Discard}
	_, err := a.resolveSudoPassword(cfg)
	if err == nil || !strings.Contains(err.Error(), "DEMO_WEB_SUDOPASS") {
		t.Fatalf("expected env remediation, got %v", err)
	}
}

func TestValidateSudoPasswordRejectsWeakValues(t *testing.T) {
	for _, value := range []string{"", "               ", "short-password", "correct horse\nbattery staple", "correct horse\rbattery staple"} {
		if err := validateSudoPassword(value); err == nil {
			t.Fatalf("expected weak password rejection for %q", value)
		}
	}
	if err := validateSudoPassword("correct horse battery staple"); err != nil {
		t.Fatalf("expected strong password, got %v", err)
	}
}

func TestLegacyConfigSudoPasswordStoresAcrossDoctorRuns(t *testing.T) {
	// Configs written before schema stamping carry a tool-forced
	// store_console_password: false; migrated loads must still store.
	dir := createTestHome(t)
	cfgPath := filepath.Join(dir, "serverpro.yaml")
	legacy := "namespace: demo\nserver: web\nadmin:\n  username: deploy\n  store_console_password: false\ncompute:\n  name: demo-web\n  location: fsn1\n  size: cx23\n  image: ubuntu-24.04\ncloudflare:\n  account_id: acc\n  tunnel:\n    name: demo-web\n"
	if err := os.WriteFile(cfgPath, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	first := &app{stdin: strings.NewReader("correct horse battery staple\n"), stdout: io.Discard}
	if _, err := first.resolveSudoPassword(cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".config", "serverpro", "namespaces", "demo", "servers", "web", "credentials.json")); err != nil {
		t.Fatal(err)
	}
	second := &app{nonInteractive: true, stdin: strings.NewReader("ignored\n"), stdout: io.Discard}
	got, err := second.resolveSudoPassword(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got != "correct horse battery staple" {
		t.Fatalf("second run password = %q", got)
	}
}

func TestResolveSudoPasswordStoresPromptedValueForLaterRuns(t *testing.T) {
	dir := createTestHome(t)
	cfg := config.ExampleServer("demo", "web")
	a := &app{stdin: strings.NewReader("correct horse battery staple\n"), stdout: io.Discard}
	if _, err := a.resolveSudoPassword(cfg); err != nil {
		t.Fatal(err)
	}
	// A fresh app with no env var and no prompt input must resolve from disk.
	fresh := &app{nonInteractive: true, stdin: strings.NewReader("ignored\n"), stdout: io.Discard}
	got, err := fresh.resolveSudoPassword(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got != "correct horse battery staple" {
		t.Fatalf("stored password = %q", got)
	}
	body, err := os.ReadFile(filepath.Join(dir, ".config", "serverpro", "namespaces", "demo", "servers", "web", "credentials.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "admin_sudo_password") {
		t.Fatalf("credentials file missing stored password:\n%s", body)
	}
}

func TestResolveSudoPasswordEnvOverridesStoredValue(t *testing.T) {
	createTestHome(t)
	cfg := config.ExampleServer("demo", "web")
	a := &app{stdin: strings.NewReader("correct horse battery staple\n"), stdout: io.Discard}
	if _, err := a.resolveSudoPassword(cfg); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEMO_WEB_SUDOPASS", "overriding stored password")
	fresh := &app{nonInteractive: true, stdin: strings.NewReader("ignored\n"), stdout: io.Discard}
	got, err := fresh.resolveSudoPassword(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got != "overriding stored password" {
		t.Fatalf("password = %q, want env value", got)
	}
}

func TestResolveSudoPasswordSkipsStorageWhenDisabled(t *testing.T) {
	dir := createTestHome(t)
	cfg := config.ExampleServer("demo", "web")
	cfg.Admin.StoreConsolePassword = false
	a := &app{stdin: strings.NewReader("correct horse battery staple\n"), stdout: io.Discard}
	if _, err := a.resolveSudoPassword(cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".config", "serverpro")); !os.IsNotExist(err) {
		t.Fatalf("disabled storage still wrote credentials: %v", err)
	}
}

func TestResolveSudoPasswordRejectsWeakStoredValue(t *testing.T) {
	createTestHome(t)
	cfg := config.ExampleServer("demo", "web")
	if err := credentials.SavePartial(cfg, credentials.Set{Namespace: "demo", Server: "web", AdminSudoPassword: "short"}); err != nil {
		t.Fatal(err)
	}
	a := &app{nonInteractive: true, stdin: strings.NewReader("ignored\n"), stdout: io.Discard}
	_, err := a.resolveSudoPassword(cfg)
	if err == nil || !strings.Contains(err.Error(), "sudo password must be at least 16 characters") {
		t.Fatalf("expected weak stored password rejection, got %v", err)
	}
}

func TestGitHubPATStoredAndReusedAcrossRuns(t *testing.T) {
	createTestHome(t)
	cfg := config.ExampleServer("demo", "web")
	if err := credentials.SavePartial(cfg, credentials.Set{Namespace: "demo", Server: "web", ServerProvider: "acct", Tailscale: "ts", Cloudflare: "cf", GitHubPAT: "ghp_stored"}); err != nil {
		t.Fatal(err)
	}
	a := &app{stdin: strings.NewReader("buzz\nbuzz@example.com\nn\n"), stdout: io.Discard}
	got, err := a.storedOrPromptedGitHubPAT(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got != "ghp_stored" {
		t.Fatalf("pat = %q, want stored value", got)
	}
}

func TestRuntimeSecretsJoinCredentialRedaction(t *testing.T) {
	a := &app{}
	a.addRuntimeSecret("correct horse battery staple")
	secrets := a.redactionSecrets(credentials.Set{Tailscale: "tailscale-secret-token"})
	got := redact.New(secrets...).String("tailscale-secret-token correct horse battery staple")
	if strings.Contains(got, "tailscale-secret-token") || strings.Contains(got, "correct horse battery staple") {
		t.Fatalf("secrets leaked after redaction: %q", got)
	}
}
