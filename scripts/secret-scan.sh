#!/usr/bin/env bash
# Local secret scan for the Git hooks. Push and PR CI no longer run, so
# credential leaks must be stopped on the developer machine before history
# becomes public. gitleaks is preferred when installed because its rule set
# is maintained upstream; the built-in patterns below are a floor that keeps
# the gate active on machines without it (installing tools needs approval).
#
# Usage: secret-scan.sh staged   (pre-commit: staged changes)
#        secret-scan.sh push     (pre-push: ref lines on stdin)
set -euo pipefail

GITLEAKS=${GITLEAKS:-gitleaks}
# All-zero object name marks a ref that does not exist on one side of a push.
ZERO_OID_RE='^0+$'
# High-confidence token shapes only; a noisy gate gets bypassed. Covers the
# credentials this project handles (GitHub PAT, Tailscale, Cloudflare via
# generic bearer/key assignments) plus private keys and cloud keys.
SECRET_PATTERNS=(
	'-----BEGIN [A-Z ]*PRIVATE KEY-----'
	'gh[pousr]_[A-Za-z0-9]{36,}'
	'github_pat_[A-Za-z0-9_]{60,}'
	'tskey-[a-z]+-[A-Za-z0-9-]{20,}'
	'AKIA[0-9A-Z]{16}'
	'xox[baprs]-[A-Za-z0-9-]{10,}'
	'(api[_-]?key|api[_-]?token|secret|password)["'\'' ]*[:=]["'\'' ]*[A-Za-z0-9/+_-]{32,}'
)

mode=${1:-}

# Added lines of a diff, prefixed with file names, for the built-in scan.
scan_diff() {
	local pattern_args=() p
	for p in "${SECRET_PATTERNS[@]}"; do pattern_args+=(-e "$p"); done
	local hits
	hits=$(grep -E '^(\+\+\+ |\+)' | grep -vE '^\+\+\+ ' | grep -nE "${pattern_args[@]}" || true)
	if [[ -n "$hits" ]]; then
		# Print only line numbers, never the matched value, so the secret does
		# not land in terminal scrollback or logs.
		echo "secret-scan: possible secret in added lines (diff line numbers): $(cut -d: -f1 <<<"$hits" | tr '\n' ' ')" >&2
		return 1
	fi
}

have_gitleaks() { command -v "$GITLEAKS" >/dev/null 2>&1; }

case "$mode" in
staged)
	if have_gitleaks; then
		exec "$GITLEAKS" git --pre-commit --staged --redact --no-banner
	fi
	echo "secret-scan: gitleaks not found; using built-in patterns" >&2
	git diff --cached -U0 --diff-filter=ACMR | scan_diff
	;;
push)
	while read -r _local_ref local_oid _remote_ref remote_oid; do
		# A deleted ref pushes no content.
		[[ $local_oid =~ $ZERO_OID_RE ]] && continue
		if [[ $remote_oid =~ $ZERO_OID_RE ]]; then
			# New branch: only commits the remote does not have yet.
			range=("$local_oid" --not --remotes)
			log_opts="$local_oid --not --remotes"
		else
			range=("$remote_oid..$local_oid")
			log_opts="$remote_oid..$local_oid"
		fi
		if have_gitleaks; then
			"$GITLEAKS" git --log-opts="$log_opts" --redact --no-banner
		else
			echo "secret-scan: gitleaks not found; using built-in patterns" >&2
			git log -p -U0 --format= "${range[@]}" | scan_diff
		fi
	done
	;;
*)
	echo "usage: $0 staged|push" >&2
	exit 2
	;;
esac
