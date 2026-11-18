//go:build darwin

package credentials

import (
	"fmt"
	"os"

	"github.com/sagmans/serverpro/internal/config"
)

// keychainDirPath is where macOS keeps the per-user keychains. /usr/bin/security
// finds the login keychain through it, so a HOME without it has no keychain.
const keychainDirPath = "~/Library/Keychains"

// systemKeyringReady refuses the macOS keyring when HOME has no keychain
// folder (service accounts, relocated or sandboxed HOMEs). WHY: in that case
// security(1) does not fail; it opens a "Keychain Not Found" dialog that
// waits for a click and offers "Reset To Defaults", which an unattended run
// must never trigger.
func systemKeyringReady() error {
	path := config.Expand(keychainDirPath)
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("%w: no keychain folder at %s", ErrKeyStoreUnavailable, path)
	}
	return nil
}
