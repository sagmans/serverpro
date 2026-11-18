package credentials

import (
	"errors"
	"fmt"
	"time"

	"github.com/zalando/go-keyring"
)

const (
	// keyringService groups serverpro items in the macOS login Keychain and
	// the Secret Service collection, so operators can find and audit them.
	keyringService = "serverpro"
	// keyringAccountPrefix scopes the item to one config root. A relocated
	// HOME (sandbox, second profile) gets its own key instead of silently
	// sharing, and possibly sealing for, another root's key.
	keyringAccountPrefix = "credential-key:"
	// keyringTimeout bounds every keyring call. The macOS backend shells out
	// to /usr/bin/security and the Linux backend talks D-Bus; neither offers
	// its own deadline, and a hung keyring must not hang an agent run.
	keyringTimeout = 10 * time.Second
)

// keyringBackend is the go-keyring surface serverpro uses. systemKeyring is a
// variable of this type so tests swap in an in-memory keyring and never touch
// the operator's login keyring.
type keyringBackend struct {
	// ready reports whether the platform keyring can be used at all without
	// a GUI dialog; nil means always ready. It runs before every call.
	ready  func() error
	get    func(service, user string) (string, error)
	set    func(service, user, secret string) error
	delete func(service, user string) error
}

var systemKeyring = keyringBackend{ready: systemKeyringReady, get: keyring.Get, set: keyring.Set, delete: keyring.Delete}

// keyringKeyStore keeps the key in the OS keyring: the login Keychain on
// macOS (unlocked with the login session, encrypted with the login password)
// and the Secret Service on Linux desktops. go-keyring passes the secret to
// /usr/bin/security on stdin, never in argv, so it never shows in `ps`.
type keyringKeyStore struct {
	account string
	backend keyringBackend
}

// Describe names the keyring item, which is what an operator inspects in
// Keychain Access or seahorse.
// Kind marks the key as held by the OS keyring.
func (keyringKeyStore) Kind() string { return KeyStoreKeyring }

func (s keyringKeyStore) Describe() string {
	return fmt.Sprintf("OS keyring item %s/%s", keyringService, s.account)
}

// Load maps a missing item to ErrKeyNotFound and every other failure to
// ErrKeyStoreUnavailable, so a locked keyring never reads as "no key".
func (s keyringKeyStore) Load() (string, error) {
	var secret string
	err := s.call(func() error {
		var err error
		secret, err = s.backend.get(keyringService, s.account)
		return err
	})
	if err != nil {
		return "", err
	}
	return secret, nil
}

// Create stores the key only when no item exists; go-keyring's Set would
// overwrite, so the existence check runs first under the lifecycle lock.
func (s keyringKeyStore) Create(identity string) error {
	_, err := s.Load()
	if err == nil {
		return ErrKeyExists
	}
	if !errors.Is(err, ErrKeyNotFound) {
		return err
	}
	return s.Replace(identity)
}

// Replace writes the item unconditionally; rotation and restore own that
// decision.
func (s keyringKeyStore) Replace(identity string) error {
	return s.call(func() error {
		return s.backend.set(keyringService, s.account, identity)
	})
}

// Delete removes the item; a missing item already means "deleted".
func (s keyringKeyStore) Delete() error {
	err := s.call(func() error {
		return s.backend.delete(keyringService, s.account)
	})
	if errors.Is(err, ErrKeyNotFound) {
		return nil
	}
	return err
}

// call checks the backend is ready before any keyring call. WHY: on macOS a
// HOME without a keychain makes /usr/bin/security open a "Keychain Not
// Found" dialog that waits for a click, which an agent run never gives;
// failing as unavailable lets automatic selection use the key file instead.
func (s keyringKeyStore) call(op func() error) error {
	if s.backend.ready != nil {
		if err := s.backend.ready(); err != nil {
			return err
		}
	}
	return withKeyringTimeout(op)
}

// withKeyringTimeout runs one keyring call under keyringTimeout and maps its
// errors onto the KeyStore contract. A timed-out call is abandoned: the CLI
// process exits soon after, which reaps the helper.
func withKeyringTimeout(call func() error) error {
	done := make(chan error, 1)
	go func() { done <- call() }()
	select {
	case err := <-done:
		return mapKeyringError(err)
	case <-time.After(keyringTimeout):
		return fmt.Errorf("%w: no answer within %s", ErrKeyStoreUnavailable, keyringTimeout)
	}
}

func mapKeyringError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, keyring.ErrNotFound):
		return ErrKeyNotFound
	default:
		return fmt.Errorf("%w: %w", ErrKeyStoreUnavailable, err)
	}
}
