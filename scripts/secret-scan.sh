#!/usr/bin/env bash
# Local secret scan for the Git hooks. Push and PR CI no longer run, so
# credential leaks must be stopped on the developer machine before history
# becomes public. gitleaks is preferred when installed because its rule set
# is maintained upstream; the built-in patterns below are a floor that keeps
# the gate active on machines without it (installing tools needs approval).
#
# Usage: secret-scan.sh staged   (pre-commit: staged changes)
#        secret-scan.sh range <local-sha> <remote-sha>  (pre-push: one pushed ref)
set -euo pipefail

GITLEAKS=${GITLEAKS:-gitleaks}
# All-zero object name: the ref does not exist on that side of the push.
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
range)
	# One pushed ref: <local sha> <remote sha>. Scans every commit the ref
	# update brings, whether or not another remote branch already has it,
	# because "already on some remote" is not proof it was ever scanned. A
	# new branch, or a remote tip this clone does not have, has no trusted
	# base, so the whole history of the pushed commit is scanned.
	(($# == 3)) || { echo "usage: $0 range <local-sha> <remote-sha>" >&2; exit 2; }
	local_sha=$2
	remote_sha=$3
	git rev-parse --verify -q "${local_sha}^{commit}" >/dev/null || { echo "secret-scan: unknown commit ${local_sha}" >&2; exit 1; }
	if [[ ! $remote_sha =~ $ZERO_OID_RE ]] && git rev-parse --verify -q "${remote_sha}^{commit}" >/dev/null; then
		log_range="${remote_sha}..${local_sha}"
	else
		log_range="$local_sha"
	fi
	if have_gitleaks; then
		exec "$GITLEAKS" git --log-opts="$log_range" --redact --no-banner
	fi
	# The built-in patterns have no reviewed ignore list, and whole-history
	# scans hit known test fixtures, so they would block every new branch.
	# Fail closed instead of narrowing the range.
	if [[ $log_range != *..* ]]; then
		echo "secret-scan: a new branch needs gitleaks for its full-history scan (install gitleaks)" >&2
		exit 1
	fi
	echo "secret-scan: gitleaks not found; using built-in patterns" >&2
	git log -p -U0 --format= "$log_range" | scan_diff
	;;
*)
	echo "usage: $0 staged|range <local-sha> <remote-sha>" >&2
	exit 2
	;;
esac
