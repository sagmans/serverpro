package credentials

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"filippo.io/age"
	"filippo.io/age/armor"
)

const (
	// credentialFileKind labels credential files in privatefile errors.
	credentialFileKind = "credentials"
	// credentialFileGlob finds every per-server credential file. It mirrors
	// config.ServerCredentialsPath so the key-loss guard sees the same files
	// the seam writes.
	credentialFileGlob = "namespaces/*/servers/*/credentials.json"
	// legacyJSONStart is the first byte of a plaintext credential file written
	// before encryption at rest; it is still read so upgrades keep working.
	legacyJSONStart = '{'
	// encryptedHeaderSlack lets the header sniff tolerate leading whitespace.
	encryptedHeaderSlack = 16
)

// encryptCredentialSet seals the set with the data key. The body is ASCII
// armored so the file is visibly an age file to anyone who opens it, and the
// age CLI can decrypt it with the recovery key if serverpro is unavailable.
// The JSON inside keeps namespace and server, which ValidateTarget checks
// after decryption, so a file copied to another server's path is rejected.
func encryptCredentialSet(creds Set) ([]byte, error) {
	identity, err := loadOrCreateIdentity()
	if err != nil {
		return nil, err
	}
	plaintext, err := json.Marshal(creds)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := sealInto(&out, identity, plaintext); err != nil {
		return nil, fmt.Errorf("encrypt credentials: %w", err)
	}
	return out.Bytes(), nil
}

// sealInto streams plaintext through age and armor. Both writers must close
// in order: age flushes its last chunk, then armor writes the footer.
func sealInto(out *bytes.Buffer, identity *age.HybridIdentity, plaintext []byte) error {
	armored := armor.NewWriter(out)
	sealed, err := age.Encrypt(armored, identity.Recipient())
	if err != nil {
		return err
	}
	if _, err := sealed.Write(plaintext); err != nil {
		return err
	}
	if err := sealed.Close(); err != nil {
		return err
	}
	return armored.Close()
}

// decodeCredentialFile turns a stored body into its JSON plaintext. Encrypted
// files need the data key; legacy plaintext passes through so hosts upgraded
// from the plaintext release keep working until their next write re-seals it.
func decodeCredentialFile(path string, body []byte) ([]byte, error) {
	trimmed := bytes.TrimSpace(body)
	if isEncryptedCredentialBody(trimmed) {
		decoded, err := openSealed(trimmed)
		if err != nil {
			return nil, fmt.Errorf("decrypt %s: %w", path, err)
		}
		return decoded, nil
	}
	if len(trimmed) > 0 && trimmed[0] == legacyJSONStart {
		return trimmed, nil
	}
	// Fail closed on anything else: guessing a format could feed attacker or
	// corrupted bytes into the JSON decoder as if they were trusted secrets.
	return nil, fmt.Errorf("credentials file %s is neither encrypted nor legacy JSON", path)
}

// openSealed decrypts one armored body with the data key. A key that does
// not match gets its own hint, because a restored wrong key otherwise shows
// only age's generic "no identity matched" error.
func openSealed(sealed []byte) ([]byte, error) {
	identity, err := loadIdentity()
	if err != nil {
		return nil, err
	}
	plaintext, err := age.Decrypt(armor.NewReader(bytes.NewReader(sealed)), identity)
	var mismatch *age.NoIdentityMatchError
	if errors.As(err, &mismatch) {
		return nil, fmt.Errorf("%w; %s holds a different key than the one that sealed this file", err, activeKeyStore.Describe())
	}
	if err != nil {
		return nil, err
	}
	return io.ReadAll(plaintext)
}

// isEncryptedCredentialBody tells sealed files from legacy plaintext by the
// armor header alone, which never occurs at the start of valid JSON.
func isEncryptedCredentialBody(body []byte) bool {
	return bytes.HasPrefix(body, []byte(armor.Header))
}

// findEncryptedCredentialFile returns one encrypted credential file under the
// serverpro config root, or "". Key creation uses it to detect a lost key.
func findEncryptedCredentialFile() (string, error) {
	matches, err := filepath.Glob(filepath.Join(credentialRoot(), credentialFileGlob))
	if err != nil {
		return "", err
	}
	for _, path := range matches {
		encrypted, err := hasEncryptedHeader(path)
		if err != nil {
			// An unreadable file cannot prove the key is unused; fail closed.
			return "", fmt.Errorf("inspect %s before creating a credential key: %w", path, err)
		}
		if encrypted {
			return path, nil
		}
	}
	return "", nil
}

// hasEncryptedHeader sniffs only the armor header. It skips the private-mode
// check on purpose: the scan reads no secret, and a loose legacy file that the
// pending write is about to tighten must not block key creation.
func hasEncryptedHeader(path string) (bool, error) {
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return false, err
	}
	defer func() { _ = file.Close() }()
	head := make([]byte, len(armor.Header)+encryptedHeaderSlack)
	n, err := io.ReadFull(file, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return false, err
	}
	return isEncryptedCredentialBody(bytes.TrimSpace(head[:n])), nil
}
