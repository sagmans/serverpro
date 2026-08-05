package lifecycle

import "github.com/sagmans/serverpro/internal/shell"

func gitDeployKeyScript(user string, repo githubSSHRepo) string {
	marker := "# serverpro git deploy access " + repo.owner + "/" + repo.name
	return `set -eu
TARGET_USER=` + shell.Quote(user) + `
KEY_NAME=` + shell.Quote(repo.deployKeyName()) + `
user_record="$(getent passwd "${TARGET_USER}")"
TARGET_HOME="$(printf '%s' "${user_record}" | cut -d: -f6)"
TARGET_GID="$(printf '%s' "${user_record}" | cut -d: -f4)"
if [ -z "${TARGET_HOME}" ] || [ ! -d "${TARGET_HOME}" ]; then
  echo "target user home missing: user=${TARGET_USER} home=${TARGET_HOME}" >&2
  exit 1
fi
ssh_dir="${TARGET_HOME}/.ssh"
key_path="${ssh_dir}/${KEY_NAME}"
config_path="${ssh_dir}/config"
known_hosts_path="${ssh_dir}/known_hosts"
install -d -m 0700 -o "${TARGET_USER}" -g "${TARGET_GID}" "${ssh_dir}"
if [ ! -f "${key_path}" ]; then
  runuser -u "${TARGET_USER}" -- ssh-keygen -q -t ed25519 -N '' -C ` + shell.Quote("serverpro deploy key "+repo.owner+"/"+repo.name) + ` -f "${key_path}"
elif [ ! -f "${key_path}.pub" ]; then
  runuser -u "${TARGET_USER}" -- sh -c 'ssh-keygen -y -f "$1" >"$1.pub"' sh "${key_path}"
fi
chown "${TARGET_USER}:${TARGET_GID}" "${key_path}" "${key_path}.pub"
chmod 0600 "${key_path}"
chmod 0644 "${key_path}.pub"
touch "${config_path}" "${known_hosts_path}"
chown "${TARGET_USER}:${TARGET_GID}" "${config_path}" "${known_hosts_path}"
chmod 0600 "${config_path}"
chmod 0644 "${known_hosts_path}"
marker=` + shell.Quote(marker) + `
if ! grep -Fxq "${marker}" "${config_path}"; then
  cat >>"${config_path}" <<EOF

${marker}
Host ` + repo.hostAlias() + `
  HostName ssh.github.com
  Port 443
  User git
  IdentityFile ~/.ssh/` + repo.deployKeyName() + `
  IdentitiesOnly yes
EOF
fi
` + gitRewriteCommand(repo) + `
github_ed25519_key='ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl'
add_known_host() {
  host_entry=$1
  known_host_line="${host_entry} ${github_ed25519_key}"
  if ! grep -Fxq "${known_host_line}" "${known_hosts_path}"; then
    printf '%s\n' "${known_host_line}" >>"${known_hosts_path}"
  fi
}
# GitHub-published Ed25519 host key, fingerprint SHA256:+DiY3wvvV6TuJJhbpZisF/zLDA0zPMSvHdkr4UvCOqU.
add_known_host 'github.com'
add_known_host 'ssh.github.com'
add_known_host '[github.com]:443'
add_known_host '[ssh.github.com]:443'
chown "${TARGET_USER}:${TARGET_GID}" "${known_hosts_path}"
chmod 0644 "${known_hosts_path}"
cat "${key_path}.pub"
`
}

func gitRewriteCommand(repo githubSSHRepo) string {
	return "runuser -u \"${TARGET_USER}\" -- env HOME=\"${TARGET_HOME}\" git config --global --replace-all " + shell.Quote("url."+repo.aliasURL()+".insteadOf") + " " + shell.Quote(repo.url)
}

func verifyGitDeployAccessScript(user, repoURL string) string {
	return `set -eu
TARGET_USER=` + shell.Quote(user) + `
REPO_URL=` + shell.Quote(repoURL) + `
user_record="$(getent passwd "${TARGET_USER}")"
TARGET_HOME="$(printf '%s' "${user_record}" | cut -d: -f6)"
if [ -z "${TARGET_HOME}" ] || [ ! -d "${TARGET_HOME}" ]; then
  echo "target user home missing: user=${TARGET_USER} home=${TARGET_HOME}" >&2
  exit 1
fi
runuser -u "${TARGET_USER}" -- env HOME="${TARGET_HOME}" GIT_SSH_COMMAND='ssh -F ~/.ssh/config -o BatchMode=yes' git ls-remote "${REPO_URL}" HEAD >/dev/null
`
}
