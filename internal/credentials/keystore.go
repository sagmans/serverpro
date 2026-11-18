package credentials

import (
	"context"
	"errors"
	"fmt"

	"filippo.io/age"
	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/privatefile"
)

const (
	// credentialKeyCreateLockPath serializes key creation, rotation, and
	// restore across concurrent serverpro processes for every store backend.
	// Racing creators would otherwise each seal files with a different key,
	// or a loser would mistake the winner's fresh ciphertext for a lost key.
	credentialKeyCreateLockPath = "~/.local/share/serverpro/credential-key.lock"
)

var (
	// ErrKeyNotFound reports that the store holds no data key yet. Readers
	// treat it as fatal for encrypted files; only writers may create a key.
	ErrKeyNotFound = errors.New("credential key not found")
	// ErrKeyExists reports that Create found a key already stored. Stores
	// must return it instead of replacing a key, so no backend can silently
	// orphan the files the old key sealed.
	ErrKeyExists = errors.New("credential key already exists")
	// ErrKeyStoreUnavailable reports a store that cannot answer right now
	// (locked keychain over SSH, no Secret Service on D-Bus, timeout). It is
	// distinct from ErrKeyNotFound so a locked keyring never reads as "no
	// key" and never triggers a replacement key next to sealed files.
	ErrKeyStoreUnavailable = errors.New("credential key store unavailable")
	// ErrKeyStoreReadOnly reports an externally supplied key (environment or
	// command). serverpro never writes those back, so CI keys stay ephemeral.
	ErrKeyStoreReadOnly = errors.New("credential key store is read-only")
)

// KeyStore holds the per-install data key that encrypts every credential
// file. WHY an interface: the key lives in a platform store (Keychain, Secret
// Service, key file, CI environment) chosen per host, while the credential
// read/write seam stays identical everywhere. No implementation may prompt;
// agents and automation must run unattended.
type KeyStore interface {
	// Describe names the store and where the key lives, for errors and
	// `auth status`.
	Describe() string
	// Kind names the backend (KeyStoreFile, KeyStoreKeyring, keyStoreEnv,
	// keyStoreCommand). Lifecycle code decides writability, file-backing,
	// and "is this a move" from it, never from concrete types or text.
	Kind() string
	// Load returns the stored age identity, ErrKeyNotFound, or
	// ErrKeyStoreUnavailable.
	Load() (string, error)
	// Create stores a new identity, or returns ErrKeyExists.
	Create(identity string) error
	// Replace overwrites the stored identity. Only rotation and restore call
	// it, under the creation lock, after every file is sealed for the new key.
	Replace(identity string) error
	// Delete removes the stored identity. Only a store move calls it, after
	// the destination store holds the same key.
	Delete() error
}

// keyStoreError adds the store description once, so every key failure names
// where the operator should look.
func keyStoreError(store KeyStore, op string, err error) error {
	return fmt.Errorf("%s credential key in %s: %w", op, store.Describe(), err)
}

// keyMayBeCreated reports load results that leave room for a first key. An
// unavailable store counts too, so a fresh headless install can fall back to
// the key file; the sealed-file scan still blocks creation on hosts that
// already have ciphertext.
func keyMayBeCreated(err error) bool {
	return errors.Is(err, ErrKeyNotFound) || errors.Is(err, ErrKeyStoreUnavailable)
}

// decryptionIdentities returns every identity that may have sealed a file:
// the active key plus a pending rotation key. WHY both: a rotation interrupted
// halfway leaves files sealed for either key, and reads must keep working
// until the rotation is resumed. The pending key is read first: a rotation
// that finishes between the two reads moves the pending key into the active
// store, so this order still sees the new key in one of them.
func decryptionIdentities() ([]age.Identity, error) {
	pending, err := loadPendingIdentity()
	if err != nil {
		return nil, err
	}
	store, err := activeKeyStore()
	if err != nil {
		return nil, err
	}
	identities := make([]age.Identity, 0, 2)
	active, activeErr := loadIdentityFrom(store)
	if activeErr == nil {
		identities = append(identities, active)
	}
	if pending != nil {
		identities = append(identities, pending)
	}
	if len(identities) == 0 {
		return nil, activeErr
	}
	return identities, nil
}

// encryptionRecipients returns the keys every new write must be readable
// by. During a rotation that is both the old and the pending key, so a write
// racing the rotation stays readable whichever key wins. It never creates a
// key: writers create it before taking any lock (withCredentialWriteLock) and
// call this under the shared lifecycle lock, where creation would deadlock.
func encryptionRecipients() ([]age.Recipient, error) {
	pending, err := loadPendingIdentity()
	if err != nil {
		return nil, err
	}
	store, err := activeKeyStore()
	if err != nil {
		return nil, err
	}
	active, err := loadIdentityFrom(store)
	if err != nil {
		return nil, err
	}
	recipients := []age.Recipient{active.Recipient()}
	if pending != nil {
		recipients = append(recipients, pending.Recipient())
	}
	return recipients, nil
}

// loadIdentityFrom parses the identity a store returns. It never creates
// one: a missing key next to existing ciphertext means the key was lost, and
// minting a fresh key would hide that instead of pointing at recovery.
func loadIdentityFrom(store KeyStore) (*age.HybridIdentity, error) {
	encoded, err := store.Load()
	if err != nil {
		return nil, keyStoreError(store, "load", err)
	}
	identity, err := parseRecoveryKey(encoded)
	if err != nil {
		return nil, keyStoreError(store, "parse", err)
	}
	return identity, nil
}

// errMalformedKey replaces age's parse error, which quotes part of the
// input; a mistyped or wrong secret must never be echoed into logs.
var errMalformedKey = errors.New("malformed credential key: expected one AGE-SECRET-KEY-PQ-1 line")

// parseRecoveryKey is the one place key text becomes an identity.
func parseRecoveryKey(encoded string) (*age.HybridIdentity, error) {
	identity, err := age.ParseHybridIdentity(encoded)
	if err != nil {
		return nil, errMalformedKey
	}
	return identity, nil
}

// loadOrCreateIdentity fetches the data key for encryption, creating it on
// first use so a fresh install encrypts from its very first write without any
// setup step. The fast path skips the lock; creation re-checks under it.
func loadOrCreateIdentity() (*age.HybridIdentity, error) {
	store, err := activeKeyStore()
	if err != nil {
		return nil, err
	}
	identity, err := loadIdentityFrom(store)
	if !keyMayBeCreated(err) {
		return identity, err
	}
	if _, err := createIdentityOnce(store); err != nil {
		return nil, err
	}
	return loadIdentityFrom(store)
}

// createIdentityOnce runs check, scan, and create as one decision under the
// creation lock and reports whether this call created the key. A process that
// waited on the lock sees the winner's key before it scans, so fresh
// ciphertext is never mistaken for a lost key. It refuses to create when
// encrypted files exist without a reachable key: they were sealed with a key
// this store cannot provide.
func createIdentityOnce(store KeyStore) (string, error) {
	unlock, err := lockKeyLifecycle()
	if err != nil {
		return "", err
	}
	defer unlock()
	_, loadErr := store.Load()
	if !keyMayBeCreated(loadErr) {
		if loadErr != nil {
			return "", keyStoreError(store, "load", loadErr)
		}
		return "", nil
	}
	existing, err := findEncryptedCredentialFile()
	if err != nil {
		return "", err
	}
	if existing != "" {
		return "", fmt.Errorf("%w: encrypted credentials exist at %s but %s cannot provide their key (%v); %s", ErrKeyNotFound, existing, store.Describe(), loadErr, missingKeyAdvice())
	}
	generated, err := age.GenerateHybridIdentity()
	if err != nil {
		return "", fmt.Errorf("generate credential key: %w", err)
	}
	if err := store.Create(generated.String()); err != nil {
		return "", keyStoreError(store, "store", err)
	}
	return generated.String(), nil
}

// missingKeyAdvice points at the repair that fits: a pending key means a
// rotation stopped after the old key was cleared, so resuming it is the fix.
func missingKeyAdvice() string {
	if pendingKeyStore().exists() {
		return "a rotation is pending; resume it with serverpro auth rotate-key"
	}
	return "restore the key with serverpro auth restore instead of creating a new one"
}

// lockKeyLifecycleShared lets writers run side by side while keeping
// rotation and restore, which hold the same lock exclusively, out.
func lockKeyLifecycleShared() (func(), error) {
	unlock, err := privatefile.LockSharedContext(context.Background(), config.Expand(credentialKeyCreateLockPath))
	if err != nil {
		return nil, fmt.Errorf("lock credential key lifecycle (shared): %w", err)
	}
	return unlock, nil
}

// lockKeyLifecycle takes the one lock that orders key creation, rotation,
// restore, and store moves.
func lockKeyLifecycle() (func(), error) {
	unlock, err := privatefile.Lock(config.Expand(credentialKeyCreateLockPath))
	if err != nil {
		return nil, fmt.Errorf("lock credential key lifecycle: %w", err)
	}
	return unlock, nil
}
