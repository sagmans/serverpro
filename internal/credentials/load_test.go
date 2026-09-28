package credentials

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/config"
)

func writeCredentialsFileFixture(t *testing.T, body string, mode os.FileMode) string {
	t.Helper()
	dir := credentialsTestHome(t)
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := config.Expand(config.ServerCredentialsPath("prod", "server"))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadJSONRejectsWorldReadableFile(t *testing.T) {
	path := writeCredentialsFileFixture(t, `{"namespace":"prod","tailscale_token":"ts","cloudflare_token":"c"}`, 0o644)
	_, err := Load(testConfig("prod", path))
	if err == nil || !strings.Contains(err.Error(), "group/world accessible") {
		t.Fatalf("expected unsafe mode error, got %v", err)
	}
}

func TestLoadJSONMigratesLegacyProjectField(t *testing.T) {
	body := `{"project":"prod","server":"server","server_provider_token":"file-h","tailscale_token":"file-ts","cloudflare_token":"file-c"}`
	path := writeCredentialsFileFixture(t, body, 0o600)
	creds, err := Load(testConfig("prod", path))
	if err != nil {
		t.Fatal(err)
	}
	if creds.Namespace != "prod" || creds.Server != "server" {
		t.Fatalf("credentials = %+v", creds)
	}
}

func TestLoadJSONRejectsDivergentNamespaceAndLegacyProject(t *testing.T) {
	body := `{"namespace":"prod","project":"other","server":"server"}`
	path := writeCredentialsFileFixture(t, body, 0o600)
	_, err := LoadPartial(testConfig("prod", path))
	if err == nil || !strings.Contains(err.Error(), `credentials namespace "prod" conflicts with legacy project "other"`) {
		t.Fatalf("error = %v", err)
	}
}

func TestLoadJSONReadsPrivateFile(t *testing.T) {
	body := `{"namespace":"prod","server":"server","server_provider_token":"file-h","tailscale_token":"file-ts","cloudflare_token":"file-c"}`
	path := writeCredentialsFileFixture(t, body, 0o600)
	creds, err := Load(testConfig("prod", path))
	if err != nil {
		t.Fatal(err)
	}
	if creds.Namespace != "prod" || creds.Server != "server" || creds.ServerProvider != "file-h" || creds.Tailscale != "file-ts" || creds.Cloudflare != "file-c" {
		t.Fatalf("bad creds: %+v", creds)
	}
}

func TestLoadPartialRejectsEmptyCredentialPath(t *testing.T) {
	_, err := LoadPartial(testConfig("prod", ""))
	if err == nil || !strings.Contains(err.Error(), "credentials.json_path") {
		t.Fatalf("expected credentials path error, got %v", err)
	}
}

func TestLoadPartialMissingFileReturnsNamespaceSet(t *testing.T) {
	credentialsTestHome(t)
	path := config.Expand(config.ServerCredentialsPath("prod", "server"))
	creds, err := LoadPartial(testConfig("prod", path))
	if err != nil {
		t.Fatal(err)
	}
	if creds.Namespace != "prod" || creds.Server != "server" {
		t.Fatalf("namespace = %q server = %q, want prod/server", creds.Namespace, creds.Server)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("missing credentials file should not be created, stat err=%v", err)
	}
}

func TestLoadRejectsDifferentNamespace(t *testing.T) {
	body := `{"namespace":"other","server":"server","server_provider_token":"file-h","tailscale_token":"file-ts","cloudflare_token":"file-c"}`
	path := writeCredentialsFileFixture(t, body, 0o600)
	_, err := Load(testConfig("prod", path))
	if err == nil || !strings.Contains(err.Error(), "namespace") {
		t.Fatalf("expected namespace mismatch, got %v", err)
	}
}

func TestLoadReadsStoredOperatorAuth(t *testing.T) {
	body := `{"namespace":"prod","server":"server","server_provider_token":"file-h","tailscale_token":"file-ts","tailscale_auth_key":"tskey-auth","cloudflare_token":"file-c","admin_sudo_password":"sudo-pass-0123456789","github_pat":"ghp_token"}`
	path := writeCredentialsFileFixture(t, body, 0o600)
	creds, err := Load(testConfig("prod", path))
	if err != nil {
		t.Fatal(err)
	}
	if creds.TSAuthKey != "tskey-auth" || creds.AdminSudoPassword != "sudo-pass-0123456789" || creds.GitHubPAT != "ghp_token" {
		t.Fatalf("stored operator auth lost: %+v", creds)
	}
}

func TestLoadPartialAcceptsTailscaleAuthKey(t *testing.T) {
	body := `{"namespace":"prod","server":"server","server_provider_token":"file-h","tailscale_token":"file-ts","tailscale_auth_key":"tskey-auth","cloudflare_token":"file-c"}`
	path := writeCredentialsFileFixture(t, body, 0o600)
	creds, err := LoadPartial(testConfig("prod", path))
	if err != nil {
		t.Fatal(err)
	}
	if creds.TSAuthKey != "tskey-auth" {
		t.Fatalf("auth key = %q", creds.TSAuthKey)
	}
}
