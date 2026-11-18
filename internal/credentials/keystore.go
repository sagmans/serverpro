package credentials

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"filippo.io/age"
	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/privatefile"
)

const (
	// credentialKeyFilePath keeps the data key outside ~/.config/serverpro so
	// a config backup, dotfile sync, or support bundle never carries the key
	// next to the ciphertext it unlocks.
	credentialKeyFilePath = "~/.local/share/serverpro/credential-key.txt"
	// credentialKeyCreateLockPath serializes first-run key creation across
	// concurrent serverpro processes for every store backend. Racing
	// creators would otherwise each seal files with a different key, or a
	// loser would mistake the winner's fresh ciphertext for a lost key.
	credentialKeyCreateLockPath = "~/.local/share/serverpro/credential-key.lock"
	credentialKeyTempPattern    = ".credential-key-*.tmp"
	credentialKeyFileKind       = "credential key"
	// credentialKeyCommentPrefix marks lines the age identity-file format
	// ignores, so the file stays readable by the age CLI for manual recovery.
	credentialKeyCommentPrefix = "#"
	credentialKeyFileHeader    = "# serverpro credential key, created %s\n" +
		"# back this file up apart from ~/.config; losing it makes stored credentials unreadable\n"
)

var (
	// ErrKeyNotFound reports that the store holds no data key yet. Readers
	// treat it as fatal for encrypted files; only writers may create a key.
	ErrKeyNotFound = errors.New("credential key not found")
	// ErrKeyExists reports that Create found a key already stored. Stores
	// must return it instead of replacing a key, so no backend can silently
	// orphan the files the old key sealed.
	ErrKeyExists = errors.New("credential key already exists")
	// errKeyFileEmpty and errKeyFileAmbiguous mark a damaged key file. They
	// are not ErrKeyNotFound, so damage never invites a key replacement.
	errKeyFileEmpty     = errors.New("credential key file holds no key")
	errKeyFileAmbiguous = errors.New("credential key file holds more than one key")
)

// KeyStore holds the per-install data key that encrypts every credential
// file. WHY an interface: the key lives in a platform store (Keychain, Secret
// Service, key file, CI command) chosen per host, while the credential
// read/write seam stays identical everywhere. No implementation may prompt;
// agents and automation must run unattended.
type KeyStore interface {
	// Describe names the store and where the key lives, for errors that
	// point the operator at recovery.
	Describe() string
	// Load returns the stored age identity, or ErrKeyNotFound.
	Load() (string, error)
	// Create stores a new identity, or returns ErrKeyExists.
	Create(identity string) error
}

// activeKeyStore is the single swap point for the data-key backend. The key
// file works on every host, so it is the default until a platform keyring is
// selected.
var activeKeyStore KeyStore = fileKeyStore{path: credentialKeyFilePath}

// keyStoreError adds the store description once, so every key failure names
// where the operator should look.
func keyStoreError(op string, err error) error {
	return fmt.Errorf("%s credential key in %s: %w", op, activeKeyStore.Describe(), err)
}

// loadIdentity fetches the data key for decryption. It never creates one: a
// missing key next to existing ciphertext means the key was lost, and minting
// a fresh key would hide that instead of pointing the operator at recovery.
func loadIdentity() (*age.HybridIdentity, error) {
	encoded, err := activeKeyStore.Load()
	if err != nil {
		return nil, keyStoreError("load", err)
	}
	identity, err := age.ParseHybridIdentity(encoded)
	if err != nil {
		return nil, keyStoreError("parse", err)
	}
	return identity, nil
}

// loadOrCreateIdentity fetches the data key for encryption, creating it on
// first use so a fresh install encrypts from its very first write without any
// setup step. The fast path skips the lock; creation re-checks under it.
func loadOrCreateIdentity() (*age.HybridIdentity, error) {
	identity, err := loadIdentity()
	if !errors.Is(err, ErrKeyNotFound) {
		return identity, err
	}
	if err := createIdentityOnce(); err != nil {
		return nil, err
	}
	return loadIdentity()
}

// createIdentityOnce runs check, scan, and create as one decision under the
// creation lock. A process that waited on the lock sees the winner's key
// before it scans, so fresh ciphertext is never mistaken for a lost key. It
// refuses to create when encrypted files exist without a key: they were sealed
// with a key this store no longer has.
func createIdentityOnce() error {
	unlock, err := privatefile.Lock(config.Expand(credentialKeyCreateLockPath))
	if err != nil {
		return keyStoreError("lock", err)
	}
	defer unlock()
	if _, err := activeKeyStore.Load(); !errors.Is(err, ErrKeyNotFound) {
		if err != nil {
			return keyStoreError("load", err)
		}
		return nil
	}
	existing, err := findEncryptedCredentialFile()
	if err != nil {
		return err
	}
	if existing != "" {
		return fmt.Errorf("%w: encrypted credentials exist at %s but %s holds no key; restore the key instead of creating a new one", ErrKeyNotFound, existing, activeKeyStore.Describe())
	}
	generated, err := age.GenerateHybridIdentity()
	if err != nil {
		return fmt.Errorf("generate credential key: %w", err)
	}
	if err := activeKeyStore.Create(generated.String()); err != nil {
		return keyStoreError("store", err)
	}
	return nil
}

// fileKeyStore keeps the identity in a 0600 file under a 0700 directory. It
// is the store that works on every host, including headless and SSH sessions
// where no desktop keyring is unlocked.
type fileKeyStore struct {
	path string
}

// Describe gives the expanded path, because recovery means restoring that
// exact file.
func (s fileKeyStore) Describe() string { return "key file " + config.Expand(s.path) }

// Load reads the key through the private-file checks, so a loosened mode or
// a symlinked key file fails closed like any other private artifact.
func (s fileKeyStore) Load() (string, error) {
	path, err := s.checkedPath("load")
	if err != nil {
		return "", err
	}
	// Checked first because a missing parent directory (first run) would
	// otherwise surface as a private-dir verification error.
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return "", ErrKeyNotFound
	}
	body, err := privatefile.ReadPrivate(path, credentialKeyFileKind)
	if err != nil {
		return "", err
	}
	return parseKeyFile(body)
}

// Create writes the key atomically in the age identity-file format, so the
// stock age CLI can use the same file for recovery. Callers hold the creation
// lock, which makes the existence check and the write one decision.
func (s fileKeyStore) Create(identity string) error {
	path, err := s.checkedPath("save")
	if err != nil {
		return err
	}
	if _, err := os.Lstat(path); err == nil {
		return ErrKeyExists
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	body := fmt.Sprintf(credentialKeyFileHeader, time.Now().UTC().Format(time.RFC3339)) + identity + "\n"
	return privatefile.AtomicWrite(path, []byte(body), privatefile.WriteOptions{TempPattern: credentialKeyTempPattern, Sync: true})
}

// checkedPath expands the key path and rejects symlinks under home, matching
// the rule credential files follow.
func (s fileKeyStore) checkedPath(action string) (string, error) {
	path := config.Expand(s.path)
	if err := privatefile.RejectSymlinkPath(path, config.Expand("~"), credentialKeyFileKind, action); err != nil {
		return "", err
	}
	return path, nil
}

// parseKeyFile accepts exactly one identity line plus comments, so a file
// edited into an ambiguous multi-key state fails closed instead of silently
// picking one.
func parseKeyFile(body []byte) (string, error) {
	var identity string
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, credentialKeyCommentPrefix) {
			continue
		}
		if identity != "" {
			return "", errKeyFileAmbiguous
		}
		identity = line
	}
	if identity == "" {
		return "", errKeyFileEmpty
	}
	return identity, nil
}
