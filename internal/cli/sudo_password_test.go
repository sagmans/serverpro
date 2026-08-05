package cli

import (
	"io"
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

func TestRuntimeSecretsJoinCredentialRedaction(t *testing.T) {
	a := &app{}
	a.addRuntimeSecret("correct horse battery staple")
	secrets := a.redactionSecrets(credentials.Set{Tailscale: "tailscale-secret-token"})
	got := redact.New(secrets...).String("tailscale-secret-token correct horse battery staple")
	if strings.Contains(got, "tailscale-secret-token") || strings.Contains(got, "correct horse battery staple") {
		t.Fatalf("secrets leaked after redaction: %q", got)
	}
}
