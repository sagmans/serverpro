package credentials

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"filippo.io/age"
	"github.com/zalando/go-keyring"

	"github.com/sagmans/serverpro/internal/config"
)

// memoryKeyring stands in for the OS keyring so the keyring store's contract
// runs on every host and CI runner without touching a real login keyring.
type memoryKeyring struct {
	mu    sync.Mutex
	items map[string]string
}

func useMemoryKeyring(t *testing.T) *memoryKeyring {
	t.Helper()
	fake := &memoryKeyring{items: map[string]string{}}
	previous := systemKeyring
	systemKeyring = keyringBackend{
		get: func(service, user string) (string, error) {
			fake.mu.Lock()
			defer fake.mu.Unlock()
			secret, ok := fake.items[service+"/"+user]
			if !ok {
				return "", keyring.ErrNotFound
			}
			return secret, nil
		},
		set: func(service, user, secret string) error {
			fake.mu.Lock()
			defer fake.mu.Unlock()
			fake.items[service+"/"+user] = secret
			return nil
		},
		delete: func(service, user string) error {
			fake.mu.Lock()
			defer fake.mu.Unlock()
			if _, ok := fake.items[service+"/"+user]; !ok {
				return keyring.ErrNotFound
			}
			delete(fake.items, service+"/"+user)
			return nil
		},
	}
	t.Cleanup(func() { systemKeyring = previous })
	return fake
}

func newTestIdentity(t *testing.T) string {
	t.Helper()
	identity, err := age.GenerateHybridIdentity()
	if err != nil {
		t.Fatal(err)
	}
	return identity.String()
}

// TestWritableKeyStoresHonorContract walks every persistent backend through
// the lifecycle rotation and restore depend on: create once, refuse a second
// create, replace, delete, and treat a missing key as not found.
func TestWritableKeyStoresHonorContract(t *testing.T) {
	credentialsTestHome(t)
	useMemoryKeyring(t)
	file, err := NamedKeyStore(KeyStoreFile)
	if err != nil {
		t.Fatal(err)
	}
	ring, err := NamedKeyStore(KeyStoreKeyring)
	if err != nil {
		t.Fatal(err)
	}
	auto := &autoKeyStore{file: defaultFileKeyStore(), keyring: defaultKeyringKeyStore()}
	for _, store := range []KeyStore{file, ring, auto} {
		t.Run(store.Describe(), func(t *testing.T) {
			requireStoreLifecycle(t, store)
		})
	}
	if _, err := NamedKeyStore("vault"); err == nil {
		t.Fatal("unknown store name accepted")
	}
}

func requireStoreLifecycle(t *testing.T, store KeyStore) {
	t.Helper()
	first, second := newTestIdentity(t), newTestIdentity(t)
	if _, err := store.Load(); !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("empty store load = %v, want ErrKeyNotFound", err)
	}
	if err := store.Create(first); err != nil {
		t.Fatal(err)
	}
	if err := store.Create(second); !errors.Is(err, ErrKeyExists) {
		t.Fatalf("second create = %v, want ErrKeyExists", err)
	}
	if err := store.Replace(second); err != nil {
		t.Fatal(err)
	}
	if got, err := store.Load(); err != nil || got != second {
		t.Fatalf("load after replace = %q, %v", got, err)
	}
	if err := store.Delete(); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(); !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("load after delete = %v, want ErrKeyNotFound", err)
	}
}

// TestExternalKeyStoresAreReadOnly proves injected keys are used but never
// written back, so CI and secret-manager keys stay owned by their source.
func TestExternalKeyStoresAreReadOnly(t *testing.T) {
	home := credentialsTestHome(t)
	identity := newTestIdentity(t)
	keyPath := filepath.Join(home, "injected-key.txt")
	if err := os.WriteFile(keyPath, []byte(identity+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(keyEnvVar, identity)
	requireReadOnlyStore(t, identity)
	t.Setenv(keyEnvVar, "")
	t.Setenv(keyCommandEnvVar, "cat "+keyPath)
	requireReadOnlyStore(t, identity)
	// A helper the command leaves running must not hold the load open.
	t.Setenv(keyCommandEnvVar, "cat "+keyPath+"; sleep 20 &")
	started := time.Now()
	if got, err := RecoveryKey(); err != nil || got != identity || time.Since(started) > keyCommandTimeout/2 {
		t.Fatalf("key command with a lingering child = %q, %v after %s", got, err, time.Since(started))
	}
	t.Setenv(keyCommandEnvVar, "exit 3")
	if _, err := RecoveryKey(); !errors.Is(err, ErrKeyStoreUnavailable) {
		t.Fatalf("failing key command = %v, want ErrKeyStoreUnavailable", err)
	}
}

func requireReadOnlyStore(t *testing.T, identity string) {
	t.Helper()
	store, err := activeKeyStore()
	if err != nil {
		t.Fatal(err)
	}
	if got, err := RecoveryKey(); err != nil || got != identity {
		t.Fatalf("%s recovery key = %q, %v", store.Describe(), got, err)
	}
	for name, op := range map[string]func() error{
		"create":  func() error { return store.Create(identity) },
		"replace": func() error { return store.Replace(identity) },
		"delete":  store.Delete,
	} {
		if err := op(); !errors.Is(err, ErrKeyStoreReadOnly) {
			t.Fatalf("%s %s = %v, want ErrKeyStoreReadOnly", store.Describe(), name, err)
		}
	}
	for _, target := range []string{"", KeyStoreFile} {
		if _, err := RotateKey(target); !errors.Is(err, ErrKeyStoreReadOnly) {
			t.Fatalf("rotate %q with %s = %v, want ErrKeyStoreReadOnly", target, store.Describe(), err)
		}
	}
	if pendingKeyStore().exists() {
		t.Fatalf("refused rotation from %s left a pending key", store.Describe())
	}
}

// TestKeyMovesIntoKeyringAndStaysReadable is the file-to-keyring move an
// operator runs once a desktop keyring is available: every file stays
// readable, the key file is removed, and unsafe moves are refused first.
func TestKeyMovesIntoKeyringAndStaysReadable(t *testing.T) {
	credentialsTestHome(t)
	fake := useMemoryKeyring(t)
	cfg := ServerConfig("prod", "web")
	if err := SetField(cfg, "cloudflare_token", "cf-move-secret"); err != nil {
		t.Fatal(err)
	}
	if _, err := RotateKey(KeyStoreKeyring); err == nil {
		t.Fatal("keyring move under a throwaway HOME was not refused")
	}
	t.Setenv(keyStoreEnvVar, KeyStoreKeyring)
	if _, err := RotateKey(KeyStoreFile); err == nil {
		t.Fatal("move away from the pinned store was not refused")
	}
	t.Setenv(keyStoreEnvVar, "")
	// A temp directory elsewhere makes this HOME look persistent, which is
	// the only place automatic selection consults the keyring.
	t.Setenv("TMPDIR", t.TempDir())
	if count, err := RotateKey(KeyStoreKeyring); err != nil || count != 1 {
		t.Fatalf("rotate into keyring = %d, %v", count, err)
	}
	if len(fake.items) != 1 {
		t.Fatalf("keyring items = %d, want 1", len(fake.items))
	}
	for _, leftover := range []string{credentialKeyFilePath, pendingKeyFilePath} {
		if _, err := os.Stat(config.Expand(leftover)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s left behind: %v", leftover, err)
		}
	}
	creds, err := LoadPartial(cfg)
	if err != nil || creds.Cloudflare != "cf-move-secret" {
		t.Fatalf("credentials after move = %+v, %v", creds, err)
	}
	if status := Status(); !status.KeyPresent || status.FileBacked || status.RotationPending {
		t.Fatalf("status after move = %+v", status)
	}
}

// TestCredentialLifecycleFromLegacyInstall is the upgrade path end to end at
// the package seam: a plaintext file from the old release, first key,
// migration, field edits, a restore that must prove itself, and the names-only
// inventory that `auth list` and doctor read.
func TestCredentialLifecycleFromLegacyInstall(t *testing.T) {
	home := credentialsTestHome(t)
	legacy := filepath.Join(home, ".config/serverpro/namespaces/prod/servers/old/credentials.json")
	if err := os.MkdirAll(filepath.Dir(legacy), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte(`{"namespace":"prod","server":"old","tailscale_token":"ts-legacy"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	key := requireInitOnce(t)
	requireMigrationOnce(t)
	requireFieldEdits(t, ServerConfig("prod", "old"))
	other := requireRestoreGuards(t, key)
	requireLostKeyAdvice(t, key, other)
}

// requireInitOnce proves the recovery key is handed out only on creation.
func requireInitOnce(t *testing.T) string {
	t.Helper()
	key, created, err := Init()
	if err != nil || !created || key == "" {
		t.Fatalf("init = created %v, %v", created, err)
	}
	if _, again, err := Init(); err != nil || again {
		t.Fatalf("second init = created %v, %v", again, err)
	}
	return key
}

// requireMigrationOnce proves migration seals the legacy file and a rerun
// finds nothing left to do.
func requireMigrationOnce(t *testing.T) {
	t.Helper()
	if plaintext, err := PlaintextFiles(); err != nil || len(plaintext) != 1 {
		t.Fatalf("plaintext files = %v, %v", plaintext, err)
	}
	if migrated, err := Migrate(); err != nil || len(migrated) != 1 || migrated[0].State != FileEncrypted {
		t.Fatalf("migrate = %+v, %v", migrated, err)
	}
	if migrated, err := Migrate(); err != nil || len(migrated) != 0 {
		t.Fatalf("second migrate = %+v, %v", migrated, err)
	}
}

// requireRestoreGuards proves restore refuses a key that opens no file and
// garbage, accepts the active key, and returns the refused key for reuse.
func requireRestoreGuards(t *testing.T, key string) string {
	t.Helper()
	other, err := age.GenerateHybridIdentity()
	if err != nil {
		t.Fatal(err)
	}
	if err := Restore(other.String(), true); err == nil {
		t.Fatal("restore accepted a key that opens no file")
	}
	if err := Restore(key, false); err != nil {
		t.Fatalf("restore of the active key = %v", err)
	}
	if err := Restore("not-a-key", false); err == nil {
		t.Fatal("restore accepted garbage")
	}
	return other.String()
}

// requireLostKeyAdvice deletes the key next to sealed files: a write must
// refuse to mint a replacement and name the repair that fits the state.
func requireLostKeyAdvice(t *testing.T, key, pending string) {
	t.Helper()
	cfg := ServerConfig("prod", "old")
	if err := defaultFileKeyStore().Delete(); err != nil {
		t.Fatal(err)
	}
	if err := SetField(cfg, "github_pat", "pat"); !errors.Is(err, ErrKeyNotFound) || !strings.Contains(err.Error(), "auth restore") {
		t.Fatalf("write with a lost key = %v, want restore advice", err)
	}
	if err := pendingKeyStore().Create(pending); err != nil {
		t.Fatal(err)
	}
	if err := SetField(cfg, "github_pat", "pat"); !errors.Is(err, ErrKeyNotFound) || !strings.Contains(err.Error(), "auth rotate-key") {
		t.Fatalf("write with a pending rotation = %v, want rotate-key advice", err)
	}
	if err := pendingKeyStore().Delete(); err != nil {
		t.Fatal(err)
	}
	if err := Restore(key, false); err != nil {
		t.Fatalf("restore after loss = %v", err)
	}
}

func requireFieldEdits(t *testing.T, cfg config.Config) {
	t.Helper()
	if err := ValidateField("github_pat"); err != nil {
		t.Fatal(err)
	}
	if err := SetField(cfg, "github_pat", "  "); err == nil {
		t.Fatal("blank value accepted")
	}
	if err := SetField(cfg, "server_provider_token", "hz-token"); err != nil {
		t.Fatal(err)
	}
	if err := SetField(cfg, "server_provider_name", "hetzner"); err != nil {
		t.Fatal(err)
	}
	if err := RemoveField(cfg, "server_provider_token"); err != nil {
		t.Fatal(err)
	}
	if err := RemoveField(cfg, "bogus"); err == nil || len(FieldNames()) != len(fieldSpecs) {
		t.Fatalf("unknown field remove = %v", err)
	}
	reports, err := Inventory()
	if err != nil || len(reports) != 1 {
		t.Fatalf("inventory = %+v, %v", reports, err)
	}
	if got := reports[0].Fields; len(got) != 1 || got[0] != "tailscale_token" {
		t.Fatalf("fields after provider token removal = %v", got)
	}
}

// TestAutoStoreFallsBackWhenKeyringRefusesWrite covers a keyring that answers
// reads but cannot store (macOS HOME without a login keychain, write timeout):
// the first key must land in the key file instead of failing the agent's run.
func TestAutoStoreFallsBackWhenKeyringRefusesWrite(t *testing.T) {
	credentialsTestHome(t)
	useMemoryKeyring(t)
	refusing := systemKeyring
	refusing.set = func(string, string, string) error { return errors.New("no login keychain") }
	systemKeyring = refusing
	t.Setenv("TMPDIR", t.TempDir())
	if err := SetField(ServerConfig("prod", "web"), "github_pat", "ghp-fallback"); err != nil {
		t.Fatal(err)
	}
	if status := Status(); !status.KeyPresent || !status.FileBacked {
		t.Fatalf("status after fallback = %+v", status)
	}
}
