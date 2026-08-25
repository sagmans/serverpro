# Release Procedure

`RELEASE.md` owns serverpro's GitHub release operation. The authoritative
implementation is `.github/workflows/release.yml`.

## Scope

A release does only the following:

1. Create a signed, annotated strict-SemVer Git tag for the approved `main`
   commit.
2. Push that one tag to `origin`.
3. Let the tag-triggered GitHub Actions workflow validate, build, attest, and
   create the immutable GitHub release.
4. Verify the published release and one non-live consumer install path.

Do not manually run `gh release create`, replace release assets, deploy an
application, change provider resources, publish to a package/container registry,
or make DNS, Cloudflare, Tailscale, or server changes as part of this procedure.

## Preconditions

- All intended work is already merged to `main`; release tags never target a
  feature or release-preparation branch.
- The local checkout is clean and synchronized with `origin/main`.
- A Git signing identity is already configured and available. SSH signing also
  needs a trusted `gpg.ssh.allowedSignersFile` before local signature
  verification can succeed. If Git configuration does not supply one, set
  `ALLOWED_SIGNERS_FILE` to a reviewed maintainer-key file; never fetch trust
  material during a release. Do not change Git configuration during a release.
- GitHub CLI authentication can read this repository and workflow runs.
- Repository settings enforce release-tag protection: the `release-tags`
  ruleset allows only repository administrators to create or delete `v*`
  tags, and immutable releases prevent modification of anything published.

## Preflight

Choose the tag before any mutation. It must pass the repository validator and
must not already exist locally, remotely, or as a GitHub release.

```sh
REPOSITORY=sagmans/serverpro
TAG=vX.Y.Z

git switch main
git fetch --tags origin
git pull --ff-only origin main
test -z "$(git status --porcelain)"
COMMIT="$(git rev-parse HEAD)"

bash scripts/validate-release-tag.sh "${TAG}"
if git show-ref --verify --quiet "refs/tags/${TAG}"; then
  printf 'local tag already exists: %s\n' "${TAG}" >&2
  exit 1
fi
if git ls-remote --exit-code --tags origin "refs/tags/${TAG}" >/dev/null; then
  printf 'remote tag already exists: %s\n' "${TAG}" >&2
  exit 1
fi
if gh release view "${TAG}" --repo "${REPOSITORY}" >/dev/null 2>&1; then
  printf 'GitHub release already exists: %s\n' "${TAG}" >&2
  exit 1
fi

make check
make test-full-chain-e2e
```

`make check` and `make test-full-chain-e2e` are both required before a tag.
They provide the non-live local release gate; do not substitute a deployment or
live-provider test.

## Create and push the signed tag

Tag the exact commit captured during preflight. `-s` makes an annotated signed
tag; never use a lightweight or unsigned tag for a release.

```sh
git tag -s "${TAG}" -m "serverpro ${TAG}" "${COMMIT}"
test "$(git rev-parse "${TAG}^{commit}")" = "${COMMIT}"
if [ -n "${ALLOWED_SIGNERS_FILE:-}" ]; then
  git -c "gpg.ssh.allowedSignersFile=${ALLOWED_SIGNERS_FILE}" tag -v "${TAG}"
else
  git tag -v "${TAG}"
fi
git push origin "refs/tags/${TAG}"
```

For SSH signatures, `git tag -v` must report a trusted signer. After the push,
confirm GitHub also recognizes the annotated tag signature:

```sh
TAG_OBJECT="$(gh api "repos/${REPOSITORY}/git/ref/tags/${TAG}" --jq '.object.sha')"
gh api "repos/${REPOSITORY}/git/tags/${TAG_OBJECT}" \
  --jq '.verification | {verified, reason}'
```

Stop if signing or verification fails. Do not push an unsigned replacement and
do not move an existing tag.

## Observe automatic GitHub publication

Pushing the tag invokes `.github/workflows/release.yml`. It reruns the
reusable CI workflow on the tagged commit, builds and smoke-tests native
Linux/macOS `amd64`/`arm64` binaries, packages deterministic archives, creates
per-target SPDX SBOMs and Sigstore provenance/SBOM attestations, then creates
the immutable GitHub release. SemVer prerelease tags become GitHub prereleases.

Before any build job runs, the `validate` job re-checks the tag server-side:
it must be an annotated tag object, carry a signature GitHub reports as
verified from a tagger identity on the release signer allowlist (override via
the `RELEASE_SIGNER_ALLOWLIST` repository variable), and point at a commit
that is an ancestor of `main`. A rejection here stops the release before any
artifact is produced.

Find the tag's run by the captured commit, then watch it to completion:

```sh
gh run list \
  --repo "${REPOSITORY}" \
  --workflow release.yml \
  --commit "${COMMIT}" \
  --event push \
  --limit 1

# Copy the displayed run database ID.
gh run watch RUN_ID --repo "${REPOSITORY}" --exit-status
```

Do not create or edit the release manually. The workflow rejects an existing
release and never clobbers assets.

## Verify the published release

After the workflow succeeds:

1. Confirm the release page and tag are correct:

   ```sh
   gh release view "${TAG}" --repo "${REPOSITORY}"
   ```

2. Confirm it contains `SHA256SUMS` plus an archive, SPDX SBOM, provenance
   bundle, and SBOM bundle for each supported target.

3. Follow [Installation — Verify release downloads](INSTALLATION.md#verify-release-downloads)
   for one supported target. Verify checksums and both attestations before
   running its binary.

4. In an isolated temporary directory, run the verified archive's
   `serverpro --version`, `serverpro --help`, and `serverpro doctor` without
   provider tokens. This is the required non-live release dogfood check.

5. Exercise the documented Go install path without installing globally:

   ```sh
   RELEASE_BIN_DIR="$(mktemp -d)"
   GOBIN="${RELEASE_BIN_DIR}" go install "github.com/sagmans/serverpro/cmd/serverpro@${TAG}"
   "${RELEASE_BIN_DIR}/serverpro" --version
   "${RELEASE_BIN_DIR}/serverpro" --help
   "${RELEASE_BIN_DIR}/serverpro" doctor
   ```

Record the tag commit, workflow URL, GitHub release URL, checks run, and
verification results in the release record or pull request.

## Failure handling

- A failed preflight or local gate stops the release before tagging.
- A signing failure stops the release; do not relax signature requirements.
- A provenance rejection (lightweight tag, missing or unverified signature,
  tagger outside the allowlist, or commit not on `main`) stops the workflow
  before build; fix the source branch and cut a fresh tag from `main`.
- For a transient GitHub Actions failure, inspect the failed run and rerun the
  failed job only when the tagged source and publication inputs remain valid.
- For a source, workflow, or release-input defect, fix it on `main`, rerun the
  required gates, and issue a new SemVer tag. Never delete, retag, force-update,
  or overwrite an existing release.
- This procedure has no deployment rollback because it performs no deployment.
