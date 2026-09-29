package credentials

import (
	"errors"
	"testing"
	"time"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/privatefile"
)

func TestUpdateMergesIntoStoredSet(t *testing.T) {
	credentialsTestHome(t)
	path := config.Expand(config.ServerCredentialsPath("prod", "server"))
	cfg := testConfig("prod", path)
	if err := SavePartial(cfg, Set{ServerProvider: "h", Tailscale: "ts"}); err != nil {
		t.Fatal(err)
	}
	if err := Update(cfg, func(current *Set) error {
		current.GitHubPAT = "ghp_merged"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	creds, err := LoadPartial(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if creds.ServerProvider != "h" || creds.Tailscale != "ts" || creds.GitHubPAT != "ghp_merged" {
		t.Fatalf("merged credentials = %+v", creds)
	}
}

// TestUpdateKeepsRotatedCredentialFromConcurrentRun is the regression guard for
// the stale-snapshot write: loading the set, prompting, and saving it whole let
// one run roll back a rotation another run had already stored.
func TestUpdateKeepsRotatedCredentialFromConcurrentRun(t *testing.T) {
	credentialsTestHome(t)
	path := config.Expand(config.ServerCredentialsPath("prod", "server"))
	cfg := testConfig("prod", path)
	if err := SavePartial(cfg, Set{ServerProvider: "h", Tailscale: "ts"}); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPartial(cfg); err != nil {
		t.Fatal(err)
	}
	if err := Update(cfg, func(current *Set) error {
		current.AdminSudoPassword = "rotated-sudo-password"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := Update(cfg, func(current *Set) error {
		current.GitHubPAT = "ghp_after_rotation"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	creds, err := LoadPartial(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if creds.AdminSudoPassword != "rotated-sudo-password" || creds.GitHubPAT != "ghp_after_rotation" || creds.Tailscale != "ts" {
		t.Fatalf("concurrent rotation lost: %+v", creds)
	}
}

func TestUpdateSurfacesMutateErrorWithoutWriting(t *testing.T) {
	credentialsTestHome(t)
	path := config.Expand(config.ServerCredentialsPath("prod", "server"))
	cfg := testConfig("prod", path)
	if err := SavePartial(cfg, Set{ServerProvider: "h"}); err != nil {
		t.Fatal(err)
	}
	want := errors.New("mutate refused")
	err := Update(cfg, func(current *Set) error {
		current.GitHubPAT = "ghp_rejected"
		return want
	})
	if !errors.Is(err, want) {
		t.Fatalf("update error = %v, want %v", err, want)
	}
	creds, err := LoadPartial(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if creds.GitHubPAT != "" {
		t.Fatalf("refused mutation was published: %+v", creds)
	}
}

// TestCredentialWritesSerializeOnWriterLock keeps two local runs from publishing
// whole-file replacements at the same time, which is what makes the merge in
// Update trustworthy rather than merely likely.
func TestCredentialWritesSerializeOnWriterLock(t *testing.T) {
	credentialsTestHome(t)
	path := config.Expand(config.ServerCredentialsPath("prod", "server"))
	cfg := testConfig("prod", path)
	if err := SavePartial(cfg, Set{ServerProvider: "h"}); err != nil {
		t.Fatal(err)
	}
	unlock, err := privatefile.Lock(path + credentialsWriterLockSuffix)
	if err != nil {
		t.Fatal(err)
	}
	updateDone := make(chan error, 1)
	saveDone := make(chan error, 1)
	go func() {
		updateDone <- Update(cfg, func(current *Set) error {
			current.Tailscale = "ts"
			return nil
		})
	}()
	go func() { saveDone <- SavePartial(cfg, Set{ServerProvider: "h2"}) }()
	for name, done := range map[string]chan error{"update": updateDone, "save": saveDone} {
		select {
		case err := <-done:
			unlock()
			t.Fatalf("%s bypassed the credential writer lock: %v", name, err)
		case <-time.After(credentialGuardProbe):
		}
	}
	unlock()
	for name, done := range map[string]chan error{"update": updateDone, "save": saveDone} {
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("%s failed after lock release: %v", name, err)
			}
		case <-time.After(time.Second):
			t.Fatalf("%s did not resume after the writer lock was released", name)
		}
	}
}
