#!/usr/bin/env bash
set -euo pipefail

# Release tags may only originate from a signed, annotated tag object whose
# commit is an ancestor of main and whose tagger identity is allowlisted;
# anything else must stop the release workflow before a build job runs.

# Maintainer identities allowed to sign release tags. Override with a
# comma-separated list via RELEASE_SIGNER_ALLOWLIST (repository variable).
DEFAULT_RELEASE_SIGNER_ALLOWLIST=ahmetsercansagman@gmail.com
MAIN_REF=refs/heads/main

fail() {
  printf 'release provenance rejected: %s\n' "${1}" >&2
  exit 1
}

if [[ $# -ne 1 ]]; then
  fail "usage: ${0##*/} <tag>"
fi
tag=$1

if [[ -z ${GITHUB_REPOSITORY:-} ]]; then
  fail "GITHUB_REPOSITORY is not set"
fi

if [[ ! $(git cat-file -t "refs/tags/${tag}" 2>/dev/null || true) == tag ]]; then
  fail "${tag} is missing locally or is not an annotated tag object"
fi

tag_object=$(git rev-parse "refs/tags/${tag}")
tag_commit=$(git rev-parse "refs/tags/${tag}^{commit}")

# A tag reachable from main proves the released source was reviewed there;
# without this check any branch commit could be published by tagging it.
git fetch --quiet origin "${MAIN_REF}"
if ! git merge-base --is-ancestor "${tag_commit}" FETCH_HEAD; then
  fail "tagged commit ${tag_commit} is not an ancestor of main"
fi

ref_object=$(gh api "repos/${GITHUB_REPOSITORY}/git/ref/tags/${tag}" --jq '.object.sha')
if [[ ${ref_object} != "${tag_object}" ]]; then
  fail "remote tag object ${ref_object} differs from local ${tag_object}"
fi

verification=$(gh api "repos/${GITHUB_REPOSITORY}/git/tags/${tag_object}")
if [[ $(jq -r '.verification.verified' <<<"${verification}") != true ]]; then
  fail "tag signature is not verified (reason: $(jq -r '.verification.reason' <<<"${verification}"))"
fi

tagger_email=$(jq -r '.tagger.email' <<<"${verification}")
allowlist=${RELEASE_SIGNER_ALLOWLIST:-${DEFAULT_RELEASE_SIGNER_ALLOWLIST}}
IFS=, read -r -a allowed_signers <<<"${allowlist}"
for allowed in "${allowed_signers[@]}"; do
  if [[ ${tagger_email} == "${allowed// /}" ]]; then
    printf 'release provenance accepted: %s signed by %s from commit %s\n' \
      "${tag}" "${tagger_email}" "${tag_commit}"
    exit 0
  fi
done
fail "tagger ${tagger_email} is not in the release signer allowlist"
