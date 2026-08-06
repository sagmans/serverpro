package lifecycle

import (
	"strconv"

	"github.com/sagmans/serverpro/internal/shell"
)

const gitConfigMissingKeyExitStatus = 5

func gitDeployKeyScript(user string, repo githubSSHRepo) string {
	marker := "# serverpro git deploy access " + repo.owner + "/" + repo.name
	return targetUserHomeScript(user) + `KEY_NAME=` + shell.Quote(repo.deployKeyName()) + `
ssh_dir="${TARGET_HOME}/.ssh"
key_path="${ssh_dir}/${KEY_NAME}"
config_path="${ssh_dir}/config"
known_hosts_path="${ssh_dir}/known_hosts"
install -d -m 0700 -o "${TARGET_USER}" -g "${TARGET_GID}" "${ssh_dir}"
` + ed25519KeyConvergenceScript("serverpro deploy key "+repo.owner+"/"+repo.name) + `touch "${config_path}" "${known_hosts_path}"
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
` + githubKnownHostsScript("github.com", "ssh.github.com", "[github.com]:443", "[ssh.github.com]:443") + `chown "${TARGET_USER}:${TARGET_GID}" "${known_hosts_path}"
chmod 0644 "${known_hosts_path}"
cat "${key_path}.pub"
`
}

func gitRewriteCommand(repo githubSSHRepo) string {
	return "runuser -u \"${TARGET_USER}\" -- env HOME=\"${TARGET_HOME}\" git config --global --replace-all " + shell.Quote("url."+repo.aliasURL()+".insteadOf") + " " + shell.Quote(repo.url)
}

func gitDeployAccessCleanupScript(repo githubSSHRepo) string {
	marker := "# serverpro git deploy access " + repo.owner + "/" + repo.name
	rewriteKey := "url." + repo.aliasURL() + ".insteadOf"
	return `deploy_marker=` + shell.Quote(marker) + `
deploy_config_tmp=
if grep -Fxq "${deploy_marker}" "${config_path}"; then
  deploy_config_tmp="$(mktemp)"
  if ! awk -v deploy_marker="${deploy_marker}" '
    $0 == deploy_marker { dropping=1; next }
    dropping && $0 == "  IdentitiesOnly yes" { dropping=0; next }
    !dropping { print }
    END { if (dropping) exit 1 }
  ' "${config_path}" >"${deploy_config_tmp}"; then
    rm -f "${deploy_config_tmp}"
    echo 'managed Git deploy SSH block is incomplete; refusing account-key migration' >&2
    exit 1
  fi
fi
deploy_rewrite_status=0
runuser -u "${TARGET_USER}" -- env HOME="${TARGET_HOME}" git config --global --unset-all ` + shell.Quote(rewriteKey) + ` || deploy_rewrite_status=$?
case "${deploy_rewrite_status}" in
  0|` + strconv.Itoa(gitConfigMissingKeyExitStatus) + `) ;;
  *) [ -z "${deploy_config_tmp}" ] || rm -f "${deploy_config_tmp}"; exit "${deploy_rewrite_status}" ;;
esac
if [ -n "${deploy_config_tmp}" ]; then
  install -m 0600 -o "${TARGET_USER}" -g "${TARGET_GID}" "${deploy_config_tmp}" "${config_path}"
  rm -f "${deploy_config_tmp}"
fi
`
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
