package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/sagmans/serverpro/internal/cloudinit"
	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/remote"
	"github.com/sagmans/serverpro/internal/shell"
	"github.com/sagmans/serverpro/internal/state"
)

// ValidateAdminPassword rejects passwords the remote script cannot carry as a
// single stdin line.
func ValidateAdminPassword(password string) error {
	if password == "" || strings.ContainsAny(password, "\r\n") {
		return errors.New("admin sudo password must be a non-empty single line")
	}
	return nil
}

// AdminPasswordInput is the protected stdin for AdminPasswordScript. The
// password never appears in the script, argv, or cloud-init user data.
func AdminPasswordInput(password string) string {
	return password + "\n"
}

// AdminPasswordScript sets the admin password with plain chpasswd so the host
// hashes it with its own PAM default (yescrypt on Ubuntu 24.04), then makes
// sudo require that password: it removes the first-boot grant and any
// cloud-init NOPASSWD:ALL rule, validates the password through sudo, and
// restores the sudoers files if any step fails. provision runs it after first
// boot and doctor --fix runs it for repair, so both share one SSH path.
func AdminPasswordScript(user string) string {
	return `set -eu
admin_user=` + shell.Quote(user) + `
IFS= read -r sudo_password
if [ -z "$sudo_password" ]; then
  echo 'admin sudo password required' >&2
  exit 1
fi
sudoers='/etc/sudoers.d/90-cloud-init-users'
bootstrap_sudoers='` + cloudinit.BootstrapSudoersPath + `'
backup_dir='/var/backups/serverpro'
backup="$backup_dir/90-cloud-init-users.before-password-sudo"
bootstrap_backup="$backup_dir/90-serverpro-bootstrap.before-password-sudo"
tmp="$(mktemp)"
cleanup() { rm -f "$tmp"; }
restore() {
  if [ -f "$backup" ]; then
    install -o root -g root -m 0440 "$backup" "$sudoers"
  fi
  if [ -f "$bootstrap_backup" ]; then
    install -o root -g root -m 0440 "$bootstrap_backup" "$bootstrap_sudoers"
  fi
}
fail_after_backup() {
  restore
  exit 1
}
trap cleanup EXIT
mkdir -p "$backup_dir"
chmod 0700 "$backup_dir"
rm -f "$backup" "$bootstrap_backup"
printf '%s:%s\n' "$admin_user" "$sudo_password" | chpasswd
if [ -f "$sudoers" ]; then
  install -o root -g root -m 0400 "$sudoers" "$backup"
  grep -Ev '^[[:space:]]*[^#].*NOPASSWD:ALL' "$sudoers" > "$tmp" || true
  if [ -s "$tmp" ]; then
    visudo -cf "$tmp" || fail_after_backup
    install -o root -g root -m 0440 "$tmp" "$sudoers" || fail_after_backup
  else
    rm -f "$sudoers" || fail_after_backup
  fi
fi
if [ -f "$bootstrap_sudoers" ]; then
  install -o root -g root -m 0400 "$bootstrap_sudoers" "$bootstrap_backup"
  rm -f "$bootstrap_sudoers" || fail_after_backup
fi
visudo -c || fail_after_backup
runuser -u "$admin_user" -- sudo -k || true
if ! printf '%s\n' "$sudo_password" | runuser -u "$admin_user" -- sudo -S -p '' -v >/dev/null 2>&1; then
  echo 'admin sudo password validation failed after fix' >&2
  fail_after_backup
fi
runuser -u "$admin_user" -- sudo -k || true
if runuser -u "$admin_user" -- sudo -n true >/dev/null 2>&1; then
  echo 'admin sudo still permits NOPASSWD:ALL after fix' >&2
  fail_after_backup
fi
printf '%s\n' 'admin password set; sudo requires it'`
}

// setRemoteAdminPassword closes the first-boot sudo grant before any other
// remote step runs with the operator's password.
func setRemoteAdminPassword(ctx context.Context, r remote.Runner, cfg config.Config, st state.State, password string) error {
	if r == nil || st.Tailscale.Name == "" {
		return nil
	}
	inputRunner, ok := r.(remote.InputRunner)
	if !ok {
		return errors.New("remote runner does not support protected stdin input")
	}
	if _, err := inputRunner.RunWithInput(ctx, cfg.Admin.Username, st.Tailscale.Name, AdminPasswordScript(cfg.Admin.Username), AdminPasswordInput(password)); err != nil {
		return fmt.Errorf("set admin password: %w", err)
	}
	return nil
}
