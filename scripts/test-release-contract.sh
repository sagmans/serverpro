#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
VALIDATOR=${ROOT}/scripts/validate-release-tag.sh
CLASSIFIER=${ROOT}/scripts/classify-release-tag.sh
PROVENANCE=${ROOT}/scripts/validate-release-provenance.sh
GO_MOD=${ROOT}/go.mod
README_DOC=${ROOT}/README.md
INSTALLATION_DOC=${ROOT}/INSTALLATION.md
DEVELOPMENT_DOC=${ROOT}/DEVELOPMENT.md
MISE_CONFIG=${ROOT}/mise.toml
GO_VERSION=1.27.4
LEGACY_GO_MINIMUM=1.26+
# Stable object identities keep command fixtures independent of local Git policy.
MAIN_COMMIT_SHA=1111111111111111111111111111111111111111
OFFMAIN_COMMIT_SHA=2222222222222222222222222222222222222222
TAG_OBJECT_SHA=3333333333333333333333333333333333333333
OFFMAIN_OBJECT_SHA=4444444444444444444444444444444444444444
MISMATCH_OBJECT_SHA=deadbeefdeadbeefdeadbeefdeadbeefdeadbeef
MAINTAINER_SIGNER=ahmetsercansagman@gmail.com
OVERRIDE_SIGNER=maintainer@example.invalid

fail() {
	printf 'FAIL | %s\n' "$1" >&2
	exit 1
}

assert_contains() {
	local file=$1
	local text=$2
	grep -Fq -- "${text}" "${file}" || fail "${file#"${ROOT}/"} missing: ${text}"
}

assert_absent() {
	local file=$1
	local text=$2
	if grep -Fq -- "${text}" "${file}"; then
		fail "${file#"${ROOT}/"} retains forbidden text: ${text}"
	fi
}

assert_line_count() {
	local file=$1
	local text=$2
	local expected=$3
	local actual
	actual=$(grep -Fxc -- "${text}" "${file}" || true)
	[[ ${actual} == "${expected}" ]] || fail "${file#"${ROOT}/"} has ${actual} exact lines ${text}; want ${expected}"
}

for tag in v0.0.0 v1.2.3 v1.2.3-alpha.1 v1.2.3+build.5 v1.2.3-rc.1+build.5; do
	bash "${VALIDATOR}" "${tag}" || fail "valid release tag rejected: ${tag}"
done
for tag in 1.2.3 v1.2 v01.2.3 v1.02.3 v1.2.03 v1.2.3-01 v1.2.3- v1.2.3+ v1.2.3-! v1.2.3.4; do
	if bash "${VALIDATOR}" "${tag}" >/dev/null 2>&1; then
		fail "invalid release tag accepted: ${tag}"
	fi
done
for case in v1.2.3:false v1.2.3+build.5:false v1.2.3-alpha.1:true v1.2.3-rc.1+build.5:true; do
	tag=${case%%:*}
	expected=${case##*:}
	actual=$(bash "${CLASSIFIER}" "${tag}") || fail "release tag classification failed: ${tag}"
	[[ ${actual} == "${expected}" ]] || fail "release tag ${tag} classified ${actual}; want ${expected}"
done

assert_line_count "${GO_MOD}" "go ${GO_VERSION}" 1
assert_line_count "${MISE_CONFIG}" "go = \"${GO_VERSION}\"" 1
assert_line_count "${README_DOC}" "If \`GOBIN\` is set, add that directory to \`PATH\` instead. Requires Go ${GO_VERSION}+." 1
assert_absent "${README_DOC}" "Requires Go ${LEGACY_GO_MINIMUM}."
assert_line_count "${INSTALLATION_DOC}" "- Go ${GO_VERSION}+" 1
assert_absent "${INSTALLATION_DOC}" "- Go ${LEGACY_GO_MINIMUM}"
assert_line_count "${DEVELOPMENT_DOC}" "- Go ${GO_VERSION} is the supported development, CI, and release toolchain." 1
# Typed workflow YAML invariants live in internal/releasecontract/workflow_test.go;
# this script covers executable release scripts and doc/toolchain pin parity.
assert_contains "${INSTALLATION_DOC}" "gh release download \"\${version}\""
assert_contains "${INSTALLATION_DOC}" "grep -F \"  \${archive}\" SHA256SUMS"
assert_contains "${INSTALLATION_DOC}" "--bundle \"\${provenance_bundle}\""
assert_contains "${INSTALLATION_DOC}" '--predicate-type https://spdx.dev/Document/v2.3'

# These command fixtures exercise the production validator without creating
# repositories, commits, or tags, so operator identity and hooks stay intact.
PROV_SANDBOX=$(mktemp -d)

# Failure paths must release the same disposable files as successful runs.
cleanup_provenance() {
	rm -r -- "${PROV_SANDBOX:?}"
}
trap cleanup_provenance EXIT

mkdir "${PROV_SANDBOX}/bin"
cat >"${PROV_SANDBOX}/bin/git" <<'SHIM'
#!/usr/bin/env bash
set -euo pipefail

# Reject unexpected commands so a fixture cannot conceal new Git mutations.
case "$*" in
  'cat-file -t refs/tags/rel-light') printf 'commit\n' ;;
  'cat-file -t refs/tags/rel-main'|'cat-file -t refs/tags/rel-offmain') printf 'tag\n' ;;
  'rev-parse refs/tags/rel-main') printf '%s\n' "${FAKE_TAG_OBJECT_SHA}" ;;
  'rev-parse refs/tags/rel-offmain') printf '%s\n' "${FAKE_OFFMAIN_OBJECT_SHA}" ;;
  'rev-parse refs/tags/rel-main^{commit}') printf '%s\n' "${FAKE_MAIN_COMMIT_SHA}" ;;
  'rev-parse refs/tags/rel-offmain^{commit}') printf '%s\n' "${FAKE_OFFMAIN_COMMIT_SHA}" ;;
  'fetch --quiet origin refs/heads/main') printf '%s\n' "${FAKE_MAIN_COMMIT_SHA}" >FETCH_HEAD ;;
  "merge-base --is-ancestor ${FAKE_MAIN_COMMIT_SHA} FETCH_HEAD")
    # A stale or omitted fetch must not satisfy the positive scenario.
    [[ $(cat FETCH_HEAD) == "${FAKE_MAIN_COMMIT_SHA}" ]]
    ;;
  "merge-base --is-ancestor ${FAKE_OFFMAIN_COMMIT_SHA} FETCH_HEAD")
    [[ $(cat FETCH_HEAD) == "${FAKE_MAIN_COMMIT_SHA}" ]]
    exit 1
    ;;
  *) printf 'unexpected fixture Git command: %s\n' "$*" >&2; exit 2 ;;
esac
SHIM
cat >"${PROV_SANDBOX}/bin/gh" <<'SHIM'
#!/usr/bin/env bash
if [[ ${1:-} == api && ${2:-} == *git/ref/tags/* ]]; then
  if [[ ${3:-} == --jq ]]; then
    printf '%s' "${FAKE_REF_SHA}"
  else
    printf '{"object":{"sha":"%s"}}' "${FAKE_REF_SHA}"
  fi
else
  printf '{"tagger":{"email":"%s"},"verification":{"verified":%s,"reason":"%s"}}' \
    "${FAKE_TAGGER_EMAIL}" "${FAKE_VERIFIED}" "${FAKE_REASON}"
fi
SHIM
chmod +x "${PROV_SANDBOX}/bin/git" "${PROV_SANDBOX}/bin/gh"

provenance_case() {
	local name=$1 expected=$2 tag=$3 ref_sha=$4 verified=$5 reason=$6 tagger=$7 diagnostic=$8
	# Production passes an unset repository variable as an empty string, so the
	# default signer list is the path most cases must prove; the override path
	# gets its own case.
	local allowlist=${9:-}
	local output
	mkdir "${PROV_SANDBOX}/${name}"
	if output=$(cd "${PROV_SANDBOX}/${name}" &&
		export GITHUB_REPOSITORY=contract/serverpro \
			PATH="${PROV_SANDBOX}/bin:${PATH}" \
			RELEASE_SIGNER_ALLOWLIST=${allowlist} \
			FAKE_MAIN_COMMIT_SHA=${MAIN_COMMIT_SHA} FAKE_OFFMAIN_COMMIT_SHA=${OFFMAIN_COMMIT_SHA} \
			FAKE_TAG_OBJECT_SHA=${TAG_OBJECT_SHA} FAKE_OFFMAIN_OBJECT_SHA=${OFFMAIN_OBJECT_SHA} \
			FAKE_REF_SHA=${ref_sha} FAKE_VERIFIED=${verified} \
			FAKE_REASON=${reason} FAKE_TAGGER_EMAIL=${tagger} &&
		bash "${PROVENANCE}" "${tag}" 2>&1); then
		[[ ${expected} == pass ]] || fail "provenance case accepted: ${name}"
	else
		[[ ${expected} == fail ]] || fail "provenance case rejected: ${name}"
	fi
	# A rejection for a different reason does not prove the intended guard.
	[[ ${output} == "${diagnostic}" ]] || fail "provenance case ${name}: ${output}"
	printf 'PASS | provenance %s\n' "${name}"
}

provenance_case lightweight-tag fail rel-light '' false unsigned '' \
	'release provenance rejected: rel-light is missing locally or is not an annotated tag object'
provenance_case annotated-off-main fail rel-offmain "${OFFMAIN_OBJECT_SHA}" true valid "${MAINTAINER_SIGNER}" \
	"release provenance rejected: tagged commit ${OFFMAIN_COMMIT_SHA} is not an ancestor of main"
provenance_case unsigned-tag fail rel-main "${TAG_OBJECT_SHA}" false unsigned "${MAINTAINER_SIGNER}" \
	'release provenance rejected: tag signature is not verified (reason: unsigned)'
provenance_case unlisted-signer fail rel-main "${TAG_OBJECT_SHA}" true valid attacker@example.invalid \
	'release provenance rejected: tagger attacker@example.invalid is not in the release signer allowlist'
provenance_case object-mismatch fail rel-main "${MISMATCH_OBJECT_SHA}" true valid "${MAINTAINER_SIGNER}" \
	"release provenance rejected: remote tag object ${MISMATCH_OBJECT_SHA} differs from local ${TAG_OBJECT_SHA}"
provenance_case signed-on-main pass rel-main "${TAG_OBJECT_SHA}" true valid "${MAINTAINER_SIGNER}" \
	"release provenance accepted: rel-main signed by ${MAINTAINER_SIGNER} from commit ${MAIN_COMMIT_SHA}"
provenance_case override-signer pass rel-main "${TAG_OBJECT_SHA}" true valid "${OVERRIDE_SIGNER}" \
	"release provenance accepted: rel-main signed by ${OVERRIDE_SIGNER} from commit ${MAIN_COMMIT_SHA}" \
	"release@example.invalid, ${OVERRIDE_SIGNER}"
provenance_case override-excludes-default fail rel-main "${TAG_OBJECT_SHA}" true valid "${MAINTAINER_SIGNER}" \
	"release provenance rejected: tagger ${MAINTAINER_SIGNER} is not in the release signer allowlist" \
	"${OVERRIDE_SIGNER}"

printf 'PASS | release contract\n'
