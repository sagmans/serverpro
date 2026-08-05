package doctor

import (
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
