package credentials

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"filippo.io/age"
)

// KeyStatus is the key-store view `auth status` and doctor report. It never
// carries the key itself.
type KeyStatus struct {
	Store           string `json:"store"`
	KeyPresent      bool   `json:"key_present"`
	FileBacked      bool   `json:"file_backed"`
	RotationPending bool   `json:"rotation_pending"`
	Error           string `json:"error,omitempty"`
}

// Status reports which store is active and whether it holds a usable key.
func Status() KeyStatus {
	store, err := activeKeyStore()
	if err != nil {
		return KeyStatus{Error: err.Error()}
	}
	status := KeyStatus{Store: store.Describe(), FileBacked: store.Kind() == KeyStoreFile, RotationPending: pendingKeyStore().exists()}
	if _, err := loadIdentityFrom(store); err != nil {
		status.Error = err.Error()
	} else {
		status.KeyPresent = true
	}
	return status
}

// Init makes sure a key exists and returns it only when this call created
// it, so the recovery key is shown once and not on every run.
func Init() (string, bool, error) {
	store, err := activeKeyStore()
	if err != nil {
		return "", false, err
	}
	if _, err := loadIdentityFrom(store); err == nil {
		return "", false, nil
	} else if !keyMayBeCreated(err) {
		return "", false, err
	}
	created, err := createIdentityOnce(store)
	if err != nil {
		return "", false, err
	}
	return created, created != "", nil
}

// RecoveryKey returns the active key in the age identity format, which is
// also what `auth restore` and `age -d -i` accept.
func RecoveryKey() (string, error) {
	store, err := activeKeyStore()
	if err != nil {
		return "", err
	}
	identity, err := loadIdentityFrom(store)
	if err != nil {
		return "", err
	}
	return identity.String(), nil
}

// Restore installs a recovery key. It first proves the key opens every
// encrypted file, so a wrong key is refused before it can replace anything.
// An existing different key is replaced only with force, and only because
// that proof shows no file depends on it. An unavailable store is refused
// rather than written: it may hold a key the operator still needs.
func Restore(encoded string, force bool) error {
	identity, err := parseRecoveryKey(encoded)
	if err != nil {
		return fmt.Errorf("parse recovery key: %w", err)
	}
	unlock, err := lockKeyLifecycle()
	if err != nil {
		return err
	}
	defer unlock()
	if err := proveIdentityOpensAll(identity); err != nil {
		return err
	}
	store, err := activeKeyStore()
	if err != nil {
		return err
	}
	current, err := store.Load()
	switch {
	case err == nil && current == identity.String():
		return nil
	case err == nil && !force:
		return fmt.Errorf("%w in %s; rerun with --force to replace it (every file opens with the recovery key)", ErrKeyExists, store.Describe())
	case err == nil:
		return store.Replace(identity.String())
	case errors.Is(err, ErrKeyNotFound):
		return store.Create(identity.String())
	default:
		return keyStoreError(store, "load", err)
	}
}

// proveIdentityOpensAll decrypts every encrypted file with one identity.
func proveIdentityOpensAll(identity age.Identity) error {
	paths, err := credentialFilePaths()
	if err != nil {
		return err
	}
	for _, path := range paths {
		encrypted, err := hasEncryptedHeader(path)
		if err != nil {
			return fmt.Errorf("inspect %s: %w", path, err)
		}
		if !encrypted {
			continue
		}
		if err := openWithIdentity(path, identity); err != nil {
			return fmt.Errorf("recovery key does not open %s: %w", path, err)
		}
	}
	return nil
}

// RotateKey seals every file for a new key and then makes it the active key,
// optionally in another store (`--store keyring` moves a file key into the
// OS keyring). The new key is saved as a pending key before any file
// changes, every write in the meantime is sealed for both keys, and the old
// key goes away last, so a crash at any step leaves every file readable and
// a rerun resumes the same rotation.
func RotateKey(target string) (int, error) {
	unlock, err := lockKeyLifecycle()
	if err != nil {
		return 0, err
	}
	defer unlock()
	current, err := activeKeyStore()
	if err != nil {
		return 0, err
	}
	// Settle the source backend before any write: once the destination
	// holds a key, automatic selection could otherwise pick it as "current".
	sourceKind := current.Kind()
	destination, err := rotationDestination(current, target)
	if err != nil {
		return 0, err
	}
	next, err := pendingIdentity()
	if err != nil {
		return 0, err
	}
	count, err := resealAllFor(next)
	if err != nil {
		return count, err
	}
	if err := destination.Replace(next.String()); err != nil {
		return count, keyStoreError(destination, "store", err)
	}
	// A move leaves the key in two stores until the old one is cleared;
	// the files already open with the new key, so the old copy is only
	// residue. current settled its backend before the write, so Delete hits
	// the store the key came from.
	if destination.Kind() != sourceKind {
		if err := current.Delete(); err != nil {
			return count, keyStoreError(current, "delete", err)
		}
	}
	return count, pendingKeyStore().Delete()
}

// rotationDestination checks the new key can actually be stored before any
// file is resealed. An injected key (environment or command) cannot be
// written back, and a move away from a pinned backend would leave the pin
// pointing at a deleted key; both would strand files sealed only for the
// pending key, so they are refused up front.
func rotationDestination(current KeyStore, target string) (KeyStore, error) {
	// An injected key cannot be cleared after a move, and every later run
	// with the variable still set would load the old key and fail to open
	// files sealed only for the new one. Importing it with restore first
	// keeps the operator's next step explicit.
	if kind := current.Kind(); kind != KeyStoreFile && kind != KeyStoreKeyring {
		return nil, fmt.Errorf("%w: the key comes from %s; rotate it where it is managed, or unset it and import the key with serverpro auth restore before rotating", ErrKeyStoreReadOnly, current.Describe())
	}
	pinned := strings.TrimSpace(os.Getenv(keyStoreEnvVar))
	if target != "" && pinned != "" && target != pinned {
		return nil, fmt.Errorf("%s pins the %q store; change or unset it before moving the key to %q", keyStoreEnvVar, pinned, target)
	}
	// Automatic selection never consults the keyring under a throwaway HOME,
	// so a key moved there would be invisible to the next run.
	if target == KeyStoreKeyring && pinned == "" && ephemeralHome() {
		return nil, fmt.Errorf("HOME is under the temp directory, where serverpro uses the key file; set %s=%s to keep the key in the keyring", keyStoreEnvVar, KeyStoreKeyring)
	}
	destination := current
	if target != "" {
		named, err := NamedKeyStore(target)
		if err != nil {
			return nil, err
		}
		destination = named
	}
	switch destination.Kind() {
	case KeyStoreFile, KeyStoreKeyring:
		return destination, nil
	default:
		return nil, fmt.Errorf("%w: %s cannot take a rotated key; rotate where the key is stored, or pass --store %s|%s", ErrKeyStoreReadOnly, destination.Describe(), KeyStoreKeyring, KeyStoreFile)
	}
}

// pendingIdentity resumes an interrupted rotation's key or starts a new one.
func pendingIdentity() (*age.HybridIdentity, error) {
	existing, err := loadPendingIdentity()
	if err != nil || existing != nil {
		return existing, err
	}
	generated, err := age.GenerateHybridIdentity()
	if err != nil {
		return nil, fmt.Errorf("generate credential key: %w", err)
	}
	if err := pendingKeyStore().Create(generated.String()); err != nil {
		return nil, keyStoreError(pendingKeyStore(), "store", err)
	}
	return generated, nil
}

// resealAllFor rewrites every credential file for one key under each file's
// writer lock. Plaintext files are sealed too, so a rotation also migrates.
// The caller holds the lifecycle lock exclusively, so only the file locks
// are taken here.
func resealAllFor(next *age.HybridIdentity) (int, error) {
	paths, err := credentialFilePaths()
	if err != nil {
		return 0, err
	}
	for i, path := range paths {
		err := withCredentialFileLock(path, func() error {
			creds, err := loadTargetedSet(path)
			if err != nil {
				return err
			}
			return resealCredentialSet(path, creds, []age.Recipient{next.Recipient()})
		})
		if err != nil {
			return i, fmt.Errorf("reseal %s: %w", path, err)
		}
	}
	return len(paths), nil
}
