package doctor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sagmans/serverpro/internal/credentials"
	"github.com/sagmans/serverpro/internal/state"
)

func TestStateSecretScanFailsOnCredentialLeak(t *testing.T) {
	res := stateSecretsClean(state.State{Namespace: "secret-token-value", Server: "default"}, credentials.Set{Tailscale: "secret-token-value"})
	if res.Status != Fail {
		t.Fatalf("expected fail, got %+v", res)
	}
}

// TestCredentialStorageCheckTracksEncryption runs the real check against a
// throwaway HOME: skipped when not injected, warn for a key file next to the
// ciphertext, fail once a plaintext file appears.
func TestCredentialStorageCheckTracksEncryption(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if res := credentialStorageResult(nil); res.Status != Skip {
		t.Fatalf("uninjected check = %+v", res)
	}
	if err := credentials.SetField(credentials.ServerConfig("prod", "web"), "cloudflare_token", "cf-doctor-secret"); err != nil {
		t.Fatal(err)
	}
	if res := credentialStorageResult(CheckCredentialStorage); res.Status != Warn {
		t.Fatalf("key file store = %+v", res)
	}
	legacy := filepath.Join(home, ".config/serverpro/namespaces/prod/servers/old/credentials.json")
	if err := os.MkdirAll(filepath.Dir(legacy), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte(`{"namespace":"prod","server":"old"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if res := CheckCredentialStorage(); res.Status != Fail || res.Remediation == "" {
		t.Fatalf("plaintext file = %+v", res)
	}
}
