package lifecycle

import (
	"strings"
	"testing"

	"github.com/sagmans/serverpro/internal/cloudinit"
)

func TestAdminPasswordScriptLetsHostHashPassword(t *testing.T) {
	script := AdminPasswordScript("deploy")
	for _, want := range []string{
		"admin_user='deploy'",
		"IFS= read -r sudo_password",
		`printf '%s:%s\n' "$admin_user" "$sudo_password" | chpasswd` + "\n",
		"bootstrap_sudoers='" + cloudinit.BootstrapSudoersPath + "'",
		`rm -f "$bootstrap_sudoers" || fail_after_backup`,
		"visudo -c || fail_after_backup",
		"sudo -S -p '' -v",
		"sudo -n true",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("admin password script missing %q:\n%s", want, script)
		}
	}
	// The host must choose the hash (yescrypt on Ubuntu 24.04); serverpro sends
	// no precomputed hash and no hashing options.
	for _, banned := range []string{"--encrypted", "chpasswd -", "--crypt-method", "$6$", "hash"} {
		if strings.Contains(script, banned) {
			t.Fatalf("admin password script should not contain %q:\n%s", banned, script)
		}
	}
}

func TestAdminPasswordInputCarriesOnlyThePassword(t *testing.T) {
	if got := AdminPasswordInput("correct horse battery staple"); got != "correct horse battery staple\n" {
		t.Fatalf("input = %q", got)
	}
	if strings.Contains(AdminPasswordScript("deploy"), "correct horse") {
		t.Fatal("script must not embed the password")
	}
}

func TestValidateAdminPasswordRejectsMultilineAndEmpty(t *testing.T) {
	for _, bad := range []string{"", "a\nb", "a\rb"} {
		if ValidateAdminPassword(bad) == nil {
			t.Fatalf("ValidateAdminPassword(%q) accepted", bad)
		}
	}
	if err := ValidateAdminPassword("correct horse: battery staple"); err != nil {
		t.Fatal(err)
	}
}
