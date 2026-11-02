#!/usr/bin/env bash
# Regenerates the serverpro-owned Pi install project in
# internal/bootstraptools/pi/. Pi 1.x publishes no npm-shrinkwrap.json, so
# without this lockfile every host would resolve Pi's transitive dependencies
# to whatever is newest at install time. The bootstrap installs this exact
# tree with `npm ci`, and doctor proves the host still matches it.
#
# Usage: scripts/update-pi-lock.sh <pi-version>
# Then set PiVersion in internal/bootstraptools/bootstrap.go to the same value.
set -euo pipefail

PI_PACKAGE=@earendil-works/pi-coding-agent
PROJECT_NAME=serverpro-pi
# Mirrors the 7-day release-age gate that WEB_SOURCES.md applies to every pin.
MIN_RELEASE_AGE_DAYS=7
# Packages that skip the release-age gate. Each entry needs a reason, and the
# reasons are repeated in WEB_SOURCES.md so a reviewer can re-check them.
RELEASE_AGE_EXEMPTIONS=(
	# Pi itself: the operator approved Pi 1.5.0 on 2026-11-02, before it aged.
	'@earendil-works/*'
	# GHSA-g3hv-75wg-m249 is fixed only in minimatch 11.0.1 and later, and
	# every 11.x release (with its new glob dependency) is younger than the gate.
	'minimatch'
	'@isaacs/nonrecursive-glob-match'
	# 3.38.1 is the first release that redacts sensitive data nested in lists
	# and maps before it reaches custom loggers (aws/aws-sdk-js-v3#8393).
	'@smithy/core'
)
REGISTRY=https://registry.npmjs.org/

version=${1:-}
if [[ ! ${version} =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
	echo "usage: scripts/update-pi-lock.sh <pi-version> (for example 1.5.0)" >&2
	exit 2
fi
command -v npm >/dev/null 2>&1 || {
	echo "update-pi-lock: npm not found; install Node.js with npm 11.10 or newer" >&2
	exit 1
}

top=$(git rev-parse --show-toplevel)
dest=${top}/internal/bootstraptools/pi
work=$(mktemp -d)
trap 'rm -rf -- "${work:?}"' EXIT

# The project only names Pi; npm resolves the rest under the release-age gate.
printf '{\n  "name": "%s",\n  "private": true,\n  "dependencies": {\n    "%s": "%s"\n  }\n}\n' \
	"${PROJECT_NAME}" "${PI_PACKAGE}" "${version}" >"${work}/package.json"

exclude_args=()
for pattern in "${RELEASE_AGE_EXEMPTIONS[@]}"; do
	exclude_args+=("--min-release-age-exclude=${pattern}")
done

# Scripts stay off: resolving the tree must never run package code.
(
	cd "${work}"
	npm install --package-lock-only --ignore-scripts --omit=dev --no-audit --no-fund \
		--registry="${REGISTRY}" --min-release-age="${MIN_RELEASE_AGE_DAYS}" "${exclude_args[@]}"
	# A known advisory blocks the update; fix it before the lock can ship.
	npm audit --omit=dev --registry="${REGISTRY}"
)

install -d "${dest}"
install -m 0644 "${work}/package.json" "${dest}/package.json"
install -m 0644 "${work}/package-lock.json" "${dest}/package-lock.json"
echo "update-pi-lock: wrote ${dest} for ${PI_PACKAGE}@${version} with npm $(npm --version)"
