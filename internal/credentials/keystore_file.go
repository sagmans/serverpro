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
	// pendingKeyFilePath holds the next key while a rotation rewrites files.
	// It persists before any file changes, so an interrupted rotation can be
	// resumed and reads never lose the key a file was sealed with.
	pendingKeyFilePath       = "~/.local/share/serverpro/credential-key.next.txt"
	credentialKeyTempPattern = ".credential-key-*.tmp"
	credentialKeyFileKind    = "credential key"
	// credentialKeyCommentPrefix marks lines the age identity-file format
	// ignores, so the file stays readable by the age CLI for manual recovery.
	credentialKeyCommentPrefix = "#"
	credentialKeyFileHeader    = "# serverpro credential key, created %s\n" +
		"# back this file up apart from ~/.config; losing it makes stored credentials unreadable\n"
)

var (
	// errKeyFileEmpty and errKeyFileAmbiguous mark a damaged key file. They
	// are not ErrKeyNotFound, so damage never invites a key replacement.
	errKeyFileEmpty     = errors.New("credential key file holds no key")
	errKeyFileAmbiguous = errors.New("credential key file holds more than one key")
)

// fileKeyStore keeps the identity in a 0600 file under a 0700 directory. It
// works on every host, including headless and SSH sessions where no desktop
// keyring is unlocked, at the cost of sharing the disk with the ciphertext.
type fileKeyStore struct {
	path string
}

// Describe gives the expanded path, because recovery means restoring that
// exact file.
func (s fileKeyStore) Describe() string { return "key file " + config.Expand(s.path) }

// Kind marks the key as sharing the disk with the ciphertext.
func (fileKeyStore) Kind() string { return KeyStoreFile }

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
// stock age CLI can use the same file for recovery. Callers hold the
// lifecycle lock, which makes the existence check and the write one decision.
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
	return s.write(path, identity)
}

// Replace swaps the key in one atomic rename, so a crash leaves either the
// old or the new key, never a torn file.
func (s fileKeyStore) Replace(identity string) error {
	path, err := s.checkedPath("save")
	if err != nil {
		return err
	}
	return s.write(path, identity)
}

// Delete removes the key durably; a missing file already means "deleted".
func (s fileKeyStore) Delete() error {
	path, err := s.checkedPath("delete")
	if err != nil {
		return err
	}
	if err := privatefile.RemoveDurably(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// exists reports whether a key file is present, without reading it. Store
// selection uses it so an existing file key always keeps priority.
func (s fileKeyStore) exists() bool {
	_, err := os.Lstat(config.Expand(s.path))
	return err == nil
}

func (s fileKeyStore) write(path, identity string) error {
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

// pendingKeyStore is where an in-progress rotation keeps its next key.
func pendingKeyStore() fileKeyStore { return fileKeyStore{path: pendingKeyFilePath} }

// loadPendingIdentity returns the rotation key if a rotation is in progress,
// or nil. A damaged pending file fails closed: files may already depend on it.
func loadPendingIdentity() (*age.HybridIdentity, error) {
	store := pendingKeyStore()
	if !store.exists() {
		return nil, nil
	}
	identity, err := loadIdentityFrom(store)
	// A rotation that finishes between the exists check and the load
	// removes the pending key; that is "no pending key", not a failure.
	if errors.Is(err, ErrKeyNotFound) {
		return nil, nil
	}
	return identity, err
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
