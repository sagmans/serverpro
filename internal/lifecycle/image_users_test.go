package lifecycle

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/state"
)

// imageUserCompute is a compute fake whose stock image declares accounts.
type imageUserCompute struct {
	*fakeHetzner
	users []string
}

func (c *imageUserCompute) ImageDefaultUsers() []string { return c.users }

func TestImageUsersRemovalScriptRefusesProtectedAccounts(t *testing.T) {
	for _, user := range []string{"root", "admin", "Linux User", "x;rm", ""} {
		if _, err := ImageUsersRemovalScript("admin", []string{user}); err == nil {
			t.Fatalf("removal of %q was not refused", user)
		}
	}
	script, err := ImageUsersRemovalScript("admin", []string{"linuxuser"})
	if err != nil {
		t.Fatal(err)
	}
	// The order is the safety argument: lock before the process check,
	// archive, sudoers with visudo, and only then delete.
	last := -1
	for _, step := range []string{"usermod -L", "pgrep -u", "tar -C", "visudo -c", "userdel -r"} {
		at := strings.Index(script, step)
		if at <= last {
			t.Fatalf("step %q out of order in:\n%s", step, script)
		}
		last = at
	}
	if !strings.Contains(script, "for user in 'linuxuser'; do") || !strings.Contains(script, ImageUsersArchiveDir) {
		t.Fatalf("script does not target the declared account:\n%s", script)
	}
}

func TestRemoveImageUsersRunsOnlyForDeclaringProviders(t *testing.T) {
	cfg := config.ExampleServer("demo", "web")
	st := state.State{Tailscale: state.TailscaleState{Name: "demo-web"}}
	r := &fakeRemote{}
	if err := removeImageUsers(context.Background(), r, &fakeHetzner{}, cfg, st); err != nil || len(r.scripts) != 0 {
		t.Fatalf("err=%v scripts=%d, want nothing for a provider without image accounts", err, len(r.scripts))
	}
	declaring := &imageUserCompute{fakeHetzner: &fakeHetzner{}, users: []string{"linuxuser"}}
	if err := removeImageUsers(context.Background(), r, declaring, cfg, st); err != nil || len(r.scripts) != 1 || !strings.Contains(r.scripts[0], "'linuxuser'") {
		t.Fatalf("err=%v scripts=%v, want one removal run", err, r.scripts)
	}
	declaring.users = []string{cfg.Admin.Username}
	if err := removeImageUsers(context.Background(), r, declaring, cfg, st); err == nil {
		t.Fatal("a provider declaring the admin user was not refused")
	}
	declaring.users = []string{"linuxuser"}
	failing := &sequenceRemote{errors: []error{errors.New("has running processes")}}
	if err := removeImageUsers(context.Background(), failing, declaring, cfg, st); err == nil || !strings.Contains(err.Error(), "remove provider image accounts") {
		t.Fatalf("err = %v, want the wrapped removal failure", err)
	}
}
