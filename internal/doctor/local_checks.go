package doctor

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/sagmans/serverpro/internal/credentials"
	"github.com/sagmans/serverpro/internal/state"
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

const (
	credentialStorageScope = "local"
	credentialStorageName  = "credential storage"
	credentialMigrateFix   = "run serverpro auth migrate, then rotate the exposed tokens"
)

// credentialStorageResult runs the injected check, or reports that this run
// did not inspect the local store.
func credentialStorageResult(check CredentialStorageCheck) Result {
	if check == nil {
		return skip(credentialStorageScope, credentialStorageName, "local credential store not inspected in this run")
	}
	return check()
}

// CheckCredentialStorage checks encryption at rest for every stored credential
// file, not just this server's. Plaintext anywhere is a failure because one
// stray copy undoes the protection for the whole install. A key file next to
// the ciphertext only warns: it is the supported fallback for headless hosts,
// but the OS keyring keeps the key off the disk that holds the files.
func CheckCredentialStorage() Result {
	plaintext, err := credentials.PlaintextFiles()
	if err != nil {
		return fail(credentialStorageScope, credentialStorageName, err.Error(), "inspect the credential files under ~/.config/serverpro")
	}
	if len(plaintext) > 0 {
		return fail(credentialStorageScope, credentialStorageName, fmt.Sprintf("%d plaintext credential file(s), first: %s", len(plaintext), plaintext[0]), credentialMigrateFix)
	}
	status := credentials.Status()
	switch {
	case status.Error != "":
		return warn(credentialStorageScope, credentialStorageName, status.Error)
	case status.FileBacked:
		return warn(credentialStorageScope, credentialStorageName, "encrypted; key kept in "+status.Store+" on the same disk (serverpro auth rotate-key --store keyring moves it when a keyring is available)")
	default:
		return pass(credentialStorageScope, credentialStorageName, "encrypted; key in "+status.Store)
	}
}
