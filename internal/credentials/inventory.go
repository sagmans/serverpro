package credentials

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"filippo.io/age"

	"github.com/sagmans/serverpro/internal/privatefile"
)

// FileState classifies one stored credential file for `auth list`,
// `auth verify`, and doctor.
type FileState string

const (
	FileEncrypted  FileState = "encrypted"
	FilePlaintext  FileState = "plaintext"
	FileUnreadable FileState = "unreadable"
)

// credentialPathNamespaceOffset and credentialPathServerOffset count path
// segments back from the file: namespaces/<namespace>/servers/<server>/credentials.json.
const (
	credentialPathNamespaceOffset = 4
	credentialPathServerOffset    = 2
)

// FileReport describes one credential file without any secret value: only
// the names of the fields that hold something.
type FileReport struct {
	Namespace string    `json:"namespace"`
	Server    string    `json:"server"`
	Path      string    `json:"path"`
	State     FileState `json:"state"`
	Fields    []string  `json:"fields,omitempty"`
	Modified  time.Time `json:"modified"`
	Error     string    `json:"error,omitempty"`
}

// Inventory reports every credential file under the config root. A file that
// cannot be read is reported, not skipped, so verify and doctor see damage.
func Inventory() ([]FileReport, error) {
	paths, err := credentialFilePaths()
	if err != nil {
		return nil, err
	}
	reports := make([]FileReport, 0, len(paths))
	for _, path := range paths {
		reports = append(reports, inspectCredentialFile(path))
	}
	return reports, nil
}

// PlaintextFiles lists legacy plaintext credential files by header alone. It
// needs no key, so doctor can flag plaintext even when the key store is down.
func PlaintextFiles() ([]string, error) {
	paths, err := credentialFilePaths()
	if err != nil {
		return nil, err
	}
	var plaintext []string
	for _, path := range paths {
		encrypted, err := hasEncryptedHeader(path)
		if err != nil {
			return nil, fmt.Errorf("inspect %s: %w", path, err)
		}
		if !encrypted {
			plaintext = append(plaintext, path)
		}
	}
	return plaintext, nil
}

// Migrate re-seals every plaintext credential file. Each file is encrypted
// in memory, decrypted again, and compared with the original before the
// atomic rename replaces the plaintext, so a migration never trades a
// readable file for a broken one. Re-running it after a crash is safe: files
// already encrypted are left alone.
func Migrate() ([]FileReport, error) {
	paths, err := credentialFilePaths()
	if err != nil {
		return nil, err
	}
	var migrated []FileReport
	for _, path := range paths {
		changed, err := migrateCredentialFile(path)
		if err != nil {
			return migrated, fmt.Errorf("migrate %s: %w", path, err)
		}
		if changed {
			migrated = append(migrated, inspectCredentialFile(path))
		}
	}
	return migrated, nil
}

func migrateCredentialFile(path string) (bool, error) {
	changed := false
	err := withCredentialWriteLock(path, func() error {
		encrypted, err := hasEncryptedHeader(path)
		if err != nil || encrypted {
			return err
		}
		creds, err := loadTargetedSet(path)
		if err != nil {
			return err
		}
		if err := resealCredentialSet(path, creds, nil); err != nil {
			return err
		}
		changed = true
		return nil
	})
	return changed, err
}

// resealCredentialSet encrypts creds (for the given recipients, or the
// active ones when nil), proves the ciphertext decrypts back to the same set,
// and only then publishes it.
func resealCredentialSet(path string, creds Set, recipients []age.Recipient) error {
	sealed, err := sealCredentialSet(creds, recipients)
	if err != nil {
		return err
	}
	if err := verifySealedSet(path, sealed, creds); err != nil {
		return err
	}
	return publishSealedCredentials(path, sealed)
}

// verifySealedSet decrypts freshly sealed bytes with the identities reads
// will use and compares the result, catching any encode/decode asymmetry
// before the original is replaced.
func verifySealedSet(path string, sealed []byte, want Set) error {
	plaintext, err := decodeCredentialFile(path, sealed)
	if err != nil {
		return fmt.Errorf("verify sealed credentials: %w", err)
	}
	var got Set
	if err := json.Unmarshal(plaintext, &got); err != nil {
		return fmt.Errorf("verify sealed credentials: %w", err)
	}
	if !reflect.DeepEqual(got, want) {
		return fmt.Errorf("verify sealed credentials: decrypted set differs from the original")
	}
	return nil
}

// loadTargetedSet reads one file and binds it to the namespace and server
// its path names, so a file copied between servers is refused, as on load.
func loadTargetedSet(path string) (Set, error) {
	namespace, server := credentialPathTarget(path)
	creds, err := loadJSON(path)
	if err != nil {
		return Set{}, err
	}
	if err := creds.ValidateTarget(namespace, server); err != nil {
		return Set{}, err
	}
	creds.Namespace = namespace
	creds.Server = server
	return creds, nil
}

func inspectCredentialFile(path string) FileReport {
	namespace, server := credentialPathTarget(path)
	report := FileReport{Namespace: namespace, Server: server, Path: path, State: FileUnreadable}
	if info, err := os.Lstat(path); err == nil {
		report.Modified = info.ModTime().UTC()
	}
	body, err := privatefile.ReadPrivate(path, credentialFileKind)
	if err != nil {
		report.Error = err.Error()
		return report
	}
	state := FilePlaintext
	if isEncryptedCredentialBody(bytes.TrimSpace(body)) {
		state = FileEncrypted
	}
	creds, err := loadTargetedSet(path)
	if err != nil {
		report.Error = err.Error()
		return report
	}
	report.State = state
	report.Fields = creds.storedFieldNames()
	return report
}

// credentialFilePaths lists credential files in a stable order, so reports
// and migrations are reproducible run to run.
func credentialFilePaths() ([]string, error) {
	paths, err := filepath.Glob(filepath.Join(credentialRoot(), credentialFileGlob))
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	return paths, nil
}

// credentialPathTarget reads namespace and server back out of the canonical
// path layout that credentialFileGlob matches.
func credentialPathTarget(path string) (string, string) {
	parts := splitPath(path)
	if len(parts) < credentialPathNamespaceOffset {
		return "", ""
	}
	return parts[len(parts)-credentialPathNamespaceOffset], parts[len(parts)-credentialPathServerOffset]
}

func splitPath(path string) []string {
	return strings.Split(filepath.ToSlash(filepath.Clean(path)), "/")
}
