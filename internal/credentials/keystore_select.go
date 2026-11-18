package credentials

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sagmans/serverpro/internal/config"
)

const (
	// keyStoreEnvVar pins the backend when automatic selection is wrong for a
	// host, for example to force the key file on a Mac used over SSH.
	keyStoreEnvVar = "SERVERPRO_KEY_STORE"
	// KeyStoreKeyring and KeyStoreFile are the persistent backends an
	// operator can pin or move the key to.
	KeyStoreKeyring = "keyring"
	KeyStoreFile    = "file"
	// keyStoreEnv and keyStoreCommand name the read-only injected backends.
	keyStoreEnv     = "env"
	keyStoreCommand = "command"
)

// activeKeyStore picks the store for this process. Order, most explicit
// first: an injected key, a key command, a pinned backend, then automatic
// selection. An unknown pin fails closed instead of guessing.
func activeKeyStore() (KeyStore, error) {
	if strings.TrimSpace(os.Getenv(keyEnvVar)) != "" {
		return envKeyStore{}, nil
	}
	if command := strings.TrimSpace(os.Getenv(keyCommandEnvVar)); command != "" {
		return commandKeyStore{command: command}, nil
	}
	pinned := strings.TrimSpace(os.Getenv(keyStoreEnvVar))
	if pinned == "" {
		return &autoKeyStore{file: defaultFileKeyStore(), keyring: defaultKeyringKeyStore()}, nil
	}
	return NamedKeyStore(pinned)
}

// NamedKeyStore resolves a persistent backend by name, for the pin variable
// and for `auth rotate-key --store`.
func NamedKeyStore(name string) (KeyStore, error) {
	switch name {
	case KeyStoreKeyring:
		return defaultKeyringKeyStore(), nil
	case KeyStoreFile:
		return defaultFileKeyStore(), nil
	default:
		return nil, fmt.Errorf("unknown credential key store %q: use %q or %q", name, KeyStoreKeyring, KeyStoreFile)
	}
}

func defaultFileKeyStore() fileKeyStore { return fileKeyStore{path: credentialKeyFilePath} }

func defaultKeyringKeyStore() keyringKeyStore {
	return keyringKeyStore{account: keyringAccountPrefix + credentialRoot(), backend: systemKeyring}
}

// autoKeyStore finds the key wherever it already lives and, for a new key,
// prefers the OS keyring. Where the key already is always wins, so selection
// is stable across runs and never splits one install over two keys. The
// choice is made once per instance: the keyring probe can cost a round trip
// to /usr/bin/security or D-Bus, and one operation must not see two answers.
type autoKeyStore struct {
	file     fileKeyStore
	keyring  keyringKeyStore
	resolved KeyStore
	// keyringDown records that the file was chosen because the keyring
	// failed, so status tells the operator why the key is not in it.
	keyringDown bool
}

// current returns the backend this instance settled on.
func (s *autoKeyStore) current() KeyStore {
	if s.resolved == nil {
		s.resolved = s.resolve()
	}
	return s.resolved
}

// Describe reports the backend that holds the key now, or the one a new key
// would go to, so `auth status` shows the store actually in use.
func (s *autoKeyStore) Describe() string {
	description := s.current().Describe()
	if s.keyringDown {
		description += " (OS keyring unavailable)"
	}
	return description
}

// Kind reports the settled backend, so lifecycle rules see the real store.
func (s *autoKeyStore) Kind() string { return s.current().Kind() }

// Load prefers an existing key file, then the settled backend, so a
// throwaway HOME never reaches the keyring. A keyring error is returned as
// is, so an unavailable keyring stays distinct from "no key".
func (s *autoKeyStore) Load() (string, error) {
	if s.file.exists() {
		return s.file.Load()
	}
	return s.current().Load()
}

// Create puts a first key in the keyring when it is usable for this HOME and
// falls back to the key file otherwise (headless Linux, locked Keychain over
// SSH, throwaway HOME). A keyring that answered reads but fails the write
// (no login keychain behind HOME, write timeout) also falls back, so a first
// run never blocks an agent. The key file then wins every later selection,
// so a keyring item written after a timeout is never used.
func (s *autoKeyStore) Create(identity string) error {
	store := s.current()
	err := store.Create(identity)
	if errors.Is(err, ErrKeyStoreUnavailable) && store.Kind() == KeyStoreKeyring {
		s.resolved, s.keyringDown = s.file, true
		return s.file.Create(identity)
	}
	return err
}

// Replace keeps the key in the backend that holds it now.
func (s *autoKeyStore) Replace(identity string) error { return s.current().Replace(identity) }

// Delete removes the key from the backend that holds it now.
func (s *autoKeyStore) Delete() error { return s.current().Delete() }

// resolve answers "which backend is this install using": the file when a key
// file exists, else the keyring when it holds a key or could take one.
func (s *autoKeyStore) resolve() KeyStore {
	if s.file.exists() {
		return s.file
	}
	if ephemeralHome() {
		return s.file
	}
	if _, err := s.keyring.Load(); err == nil || errors.Is(err, ErrKeyNotFound) {
		return s.keyring
	}
	s.keyringDown = true
	return s.file
}

// ephemeralHome reports a HOME inside the temp directory. Test suites,
// sandboxes, and dogfood harnesses run that way; their keys must not
// accumulate in the operator's persistent login keyring.
func ephemeralHome() bool {
	home := resolvedPath(config.Expand("~"))
	temp := resolvedPath(os.TempDir())
	if home == "" || temp == "" {
		return false
	}
	return home == temp || strings.HasPrefix(home, temp+string(filepath.Separator))
}

// resolvedPath follows symlinks so /var and /private/var on macOS compare
// equal; an unresolvable path falls back to its cleaned form.
func resolvedPath(path string) string {
	if path == "" {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return filepath.Clean(resolved)
	}
	return filepath.Clean(path)
}
