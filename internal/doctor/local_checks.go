package doctor

import (
	"encoding/json"
	"os/exec"
	"strings"

	"github.com/assagman/serverpro/internal/credentials"
	"github.com/assagman/serverpro/internal/state"
)

func localTool(name string) Result {
	if _, err := exec.LookPath(name); err != nil {
		return warn("local", name, "not found")
	}
	return pass("local", name, "found")
}

func stateSecretsClean(st state.State, creds credentials.Set) Result {
	b, err := json.Marshal(st)
	if err != nil {
		return fail("state", "secret scan", err.Error(), "inspect state file")
	}
	body := string(b)
	for _, secret := range creds.Secrets() {
		if len(secret) >= 8 && strings.Contains(body, secret) {
			return fail("state", "secret scan", "state contains credential-like value", "rotate secret and delete state copy")
		}
	}
	return pass("state", "secret scan", "no configured credentials in state")
}
