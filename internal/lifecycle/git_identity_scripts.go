package lifecycle

import (
	"strings"

	"github.com/sagmans/serverpro/internal/config"
	"github.com/sagmans/serverpro/internal/shell"
)

// githubEd25519HostKey is GitHub's published host key; pinning it avoids
// trusting a live key scan that would accept a MITM key on first connect.
const githubEd25519HostKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl"

func githubKnownHostsScript(hosts ...string) string {
	var b strings.Builder
	b.WriteString("github_ed25519_key='" + githubEd25519HostKey + "'\n")
	b.WriteString(`add_known_host() {
  host_entry=$1
  known_host_line="${host_entry} ${github_ed25519_key}"
  if ! grep -Fxq "${known_host_line}" "${known_hosts_path}"; then
    printf '%s\n' "${known_host_line}" >>"${known_hosts_path}"
  fi
}
`)
	b.WriteString("# GitHub-published Ed25519 host key, fingerprint SHA256:+DiY3wvvV6TuJJhbpZisF/zLDA0zPMSvHdkr4UvCOqU.\n")
	for _, host := range hosts {
		b.WriteString("add_known_host '" + host + "'\n")
	}
	return b.String()
}

// targetUserHomeScript resolves the admin home/gid once so every git setup
// snippet can install files with correct ownership instead of leaking root
// ownership into the user's ssh/git configuration.
func targetUserHomeScript(user string) string {
	return `set -eu
TARGET_USER=` + shell.Quote(user) + `
user_record="$(getent passwd "${TARGET_USER}")"
TARGET_HOME="$(printf '%s' "${user_record}" | cut -d: -f6)"
TARGET_GID="$(printf '%s' "${user_record}" | cut -d: -f4)"
if [ -z "${TARGET_HOME}" ] || [ ! -d "${TARGET_HOME}" ]; then
  echo "target user home missing: user=${TARGET_USER} home=${TARGET_HOME}" >&2
  exit 1
fi
`
}

func ed25519KeyConvergenceScript(comment string) string {
	return `if [ ! -f "${key_path}" ]; then
  runuser -u "${TARGET_USER}" -- ssh-keygen -q -t ed25519 -N '' -C ` + shell.Quote(comment) + ` -f "${key_path}"
elif [ ! -f "${key_path}.pub" ]; then
  runuser -u "${TARGET_USER}" -- sh -c 'ssh-keygen -y -f "$1" >"$1.pub"' sh "${key_path}"
fi
chown "${TARGET_USER}:${TARGET_GID}" "${key_path}" "${key_path}.pub"
chmod 0600 "${key_path}"
chmod 0644 "${key_path}.pub"
`
}

func gitAccountKeyScript(user string, deployRepo *githubSSHRepo) string {
	deployCleanup := ""
	if deployRepo != nil {
		deployCleanup = gitDeployAccessCleanupScript(*deployRepo)
	}
	return targetUserHomeScript(user) + `
ssh_dir="${TARGET_HOME}/.ssh"
key_path="${ssh_dir}/id_ed25519"
config_path="${ssh_dir}/config"
known_hosts_path="${ssh_dir}/known_hosts"
install -d -m 0700 -o "${TARGET_USER}" -g "${TARGET_GID}" "${ssh_dir}"
` + ed25519KeyConvergenceScript("serverpro account key "+user) + `touch "${config_path}" "${known_hosts_path}"
chown "${TARGET_USER}:${TARGET_GID}" "${config_path}" "${known_hosts_path}"
chmod 0600 "${config_path}"
chmod 0644 "${known_hosts_path}"
` + deployCleanup + `marker='# serverpro github account access'
if ! grep -Fxq "${marker}" "${config_path}"; then
  cat >>"${config_path}" <<EOF

${marker}
Host github.com
  HostName github.com
  User git
  IdentityFile ~/.ssh/id_ed25519
  IdentitiesOnly yes
EOF
fi
` + githubKnownHostsScript("github.com") + `
chown "${TARGET_USER}:${TARGET_GID}" "${known_hosts_path}"
chmod 0644 "${known_hosts_path}"
cat "${key_path}.pub"
`
}

// verifyGitHubSSHScript treats "successfully authenticated" as success because
// GitHub's SSH endpoint always exits 1, even for valid auth.
func verifyGitHubSSHScript(user string) string {
	return targetUserHomeScript(user) + `
out="$(runuser -u "${TARGET_USER}" -- env HOME="${TARGET_HOME}" ssh -o BatchMode=yes -o StrictHostKeyChecking=yes -T git@github.com 2>&1)" && status=0 || status=$?
printf '%s\n' "${out}"
case "${out}" in
  *"successfully authenticated"*) exit 0 ;;
esac
echo "GitHub SSH authentication failed" >&2
exit "${status:-1}"
`
}

func gitIdentityScript(user string, identity config.GitIdentity) string {
	return targetUserHomeScript(user) +
		"runuser -u \"${TARGET_USER}\" -- env HOME=\"${TARGET_HOME}\" git config --global user.name " + shell.Quote(identity.Name) + "\n" +
		"runuser -u \"${TARGET_USER}\" -- env HOME=\"${TARGET_HOME}\" git config --global user.email " + shell.Quote(identity.Email) + "\n"
}

func gitSigningKeyScript(user string) string {
	return targetUserHomeScript(user) + `
ssh_dir="${TARGET_HOME}/.ssh"
key_path="${ssh_dir}/id_ed25519_sign"
install -d -m 0700 -o "${TARGET_USER}" -g "${TARGET_GID}" "${ssh_dir}"
` + ed25519KeyConvergenceScript("serverpro signing key "+user) + `git_config() { runuser -u "${TARGET_USER}" -- env HOME="${TARGET_HOME}" git config --global "$@"; }
git_config gpg.format ssh
git_config user.signingkey "${key_path}.pub"
git_config commit.gpgsign true
cat "${key_path}.pub"
`
}

// ghTokenScript reads the PAT from stdin (never argv/script text) and stores
// it root-protected; git_protocol ssh keeps gh repo operations on SSH.
func ghTokenScript(user string) string {
	return targetUserHomeScript(user) + `
IFS= read -r GH_PAT
if [ -z "${GH_PAT}" ]; then
  echo 'GitHub PAT required on stdin' >&2
  exit 1
fi
mise_bin="${TARGET_HOME}/.local/bin/mise"
gh_exec() { runuser -u "${TARGET_USER}" -- env HOME="${TARGET_HOME}" GH_TOKEN="${GH_PAT}" "${mise_bin}" exec -- gh "$@"; }
login="$(gh_exec api user --jq .login)" || { echo 'GitHub PAT validation failed' >&2; exit 1; }
gh_dir="${TARGET_HOME}/.config/gh"
install -d -m 0700 -o "${TARGET_USER}" -g "${TARGET_GID}" "${gh_dir}"
hosts_yml="${gh_dir}/hosts.yml"
cat >"${hosts_yml}" <<EOF
github.com:
    user: ${login}
    oauth_token: ${GH_PAT}
    git_protocol: ssh
EOF
chown "${TARGET_USER}:${TARGET_GID}" "${hosts_yml}"
chmod 0600 "${hosts_yml}"
gh_exec auth status >/dev/null
printf 'gh authenticated as %s\n' "${login}"
`
}
