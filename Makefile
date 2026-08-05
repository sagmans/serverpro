SHELL := /bin/sh

GO ?= go
BIN_DIR := .bin
BINARY ?= serverpro
LINK_BIN_DIR ?= $(HOME)/.local/bin
LINK_BIN := $(LINK_BIN_DIR)/$(BINARY)
GOLANGCI_LINT_VERSION := v1.64.5
GOVULNCHECK_VERSION := v1.3.0
GOLANGCI_LINT := $(BIN_DIR)/golangci-lint
GOVULNCHECK := $(BIN_DIR)/govulncheck
GOLANGCI_LINT_STAMP := $(BIN_DIR)/.golangci-lint-$(GOLANGCI_LINT_VERSION)
GOVULNCHECK_STAMP := $(BIN_DIR)/.govulncheck-$(GOVULNCHECK_VERSION)
GOLANGCI_LINT_CACHE := $(CURDIR)/.cache/golangci-lint
ABS_BIN_DIR := $(abspath $(BIN_DIR))
COVERAGE_PROFILE := coverage.out
MIN_COVERAGE := 81.8
HARNESS_ENV_SENTINEL := serverpro-no-token-sentinel

.PHONY: ci check fmt fmt-check tidy-check test test-unit test-go-check test-harness test-smoke test-integration test-e2e test-full-chain-e2e test-release test-release-shell test-dogfood-readonly test-dogfood-live test-dogfood-live-selftest vet build bin dogfood-no-token link-bin lint vuln race cover install-tools install-hooks gen-bootstrap-wrapper
.SILENT: fmt fmt-check link-bin

ci: build check

check: fmt-check tidy-check test-go-check test-harness test-smoke test-e2e test-release-shell test-dogfood-live-selftest vet lint vuln

fmt:
	bash scripts/go-format.sh write

fmt-check:
	bash scripts/go-format.sh check

tidy-check:
	$(GO) mod tidy -diff

test: test-unit

test-unit:
	"${GO}" test ./...

test-go-check:
	"${GO}" test -race -covermode=atomic -coverprofile="${COVERAGE_PROFILE}" ./...
	GO="${GO}" bash scripts/check-coverage.sh "${COVERAGE_PROFILE}" "${MIN_COVERAGE}"

test-harness:
	bash scripts/test-coverage-policy.sh
	bash scripts/test-make-gates.sh

test-smoke: bin
	"./${BINARY}" --version >/dev/null
	"./${BINARY}" --help >/dev/null
	"./${BINARY}" doctor >/dev/null

test-integration:
	"${GO}" test ./internal/cli ./internal/lifecycle ./internal/provider/... ./internal/doctor ./internal/importsync ./internal/state ./internal/credentials

test-e2e: dogfood-no-token

test-full-chain-e2e:
	GOFLAGS="-tags=serverpro_full_chain_e2e" go test ./internal/e2e

test-release: test-release-shell
	"${GO}" test ./internal/releasecontract

test-release-shell:
	bash scripts/test-release-contract.sh

test-dogfood-readonly: dogfood-no-token

test-dogfood-live: bin
	SERVERPRO_BIN="./${BINARY}" bash scripts/test-dogfood-live.sh

# Network-free proof that the live harness guards, secret transport, and
# cleanup retention work; safe for `check` because it uses only fakes.
test-dogfood-live-selftest:
	python3 -m unittest scripts/test_dogfood_validate.py
	bash scripts/test-dogfood-live-selftest.sh

vet:
	"${GO}" vet ./...

build:
	"${GO}" build ./...

bin:
	"${GO}" build -o "./${BINARY}" ./cmd/serverpro

dogfood-no-token: bin
	SERVERPRO_SERVER_PROVIDER_TOKEN="$(HARNESS_ENV_SENTINEL)" SERVER_PROVIDER_TOKEN="$(HARNESS_ENV_SENTINEL)" \
	SERVERPRO_TAILSCALE_TOKEN="$(HARNESS_ENV_SENTINEL)" TAILSCALE_API_TOKEN="$(HARNESS_ENV_SENTINEL)" \
	SERVERPRO_CLOUDFLARE_TOKEN="$(HARNESS_ENV_SENTINEL)" CLOUDFLARE_API_TOKEN="$(HARNESS_ENV_SENTINEL)" \
	SERVERPRO_BIN="./${BINARY}" bash scripts/test-cli-no-token-surface.sh

# Regenerate the manual bootstrap wrapper from the Go pin manifest; the drift
# gate test fails in CI when the checked-in wrapper is stale.
gen-bootstrap-wrapper:
	$(GO) run ./cmd/genbootstrapwrapper > scripts/serverpro-bootstrap-tools.sh

link-bin: bin
	mkdir -p "${LINK_BIN_DIR}"
	ln -sf "${CURDIR}/${BINARY}" "${LINK_BIN}"
	case ":$$PATH:" in *":${LINK_BIN_DIR}:"*) ;; *) echo "warning: ${LINK_BIN_DIR} is not on PATH"; esac
	echo "linked ${LINK_BIN} -> ${CURDIR}/${BINARY}"

lint: ${GOLANGCI_LINT_STAMP}
	GOLANGCI_LINT_CACHE="${GOLANGCI_LINT_CACHE}" "${GOLANGCI_LINT}" run

vuln: ${GOVULNCHECK_STAMP}
	"${GOVULNCHECK}" ./...

race:
	"${GO}" test -race ./...

cover:
	"${GO}" test -coverprofile="${COVERAGE_PROFILE}" ./...
	GO="${GO}" bash scripts/check-coverage.sh "${COVERAGE_PROFILE}" "${MIN_COVERAGE}"

install-tools: ${GOLANGCI_LINT_STAMP} ${GOVULNCHECK_STAMP}

install-hooks:
	mkdir -p "$$(git rev-parse --git-common-dir)/hooks"
	cp .githooks/pre-commit "$$(git rev-parse --git-common-dir)/hooks/pre-commit"
	chmod +x "$$(git rev-parse --git-common-dir)/hooks/pre-commit"

${GOLANGCI_LINT_STAMP}:
	mkdir -p "${BIN_DIR}"
	rm -f "${GOLANGCI_LINT}"
	GOBIN="${ABS_BIN_DIR}" "${GO}" install "github.com/golangci/golangci-lint/cmd/golangci-lint@${GOLANGCI_LINT_VERSION}"
	touch "$@"

${GOVULNCHECK_STAMP}:
	mkdir -p "${BIN_DIR}"
	rm -f "${GOVULNCHECK}"
	GOBIN="${ABS_BIN_DIR}" "${GO}" install "golang.org/x/vuln/cmd/govulncheck@${GOVULNCHECK_VERSION}"
	touch "$@"
