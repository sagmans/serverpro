SHELL := /bin/sh

GO ?= go
BIN_DIR := .bin
BINARY ?= serverpro
LINK_BIN_DIR ?= $(HOME)/.local/bin
LINK_BIN := $(LINK_BIN_DIR)/$(BINARY)
GOLANGCI_LINT_VERSION := v1.64.8
GOVULNCHECK_VERSION := v1.5.0
GOLANGCI_LINT := $(BIN_DIR)/golangci-lint
GOVULNCHECK := $(BIN_DIR)/govulncheck
GOLANGCI_LINT_STAMP := $(BIN_DIR)/.golangci-lint-$(GOLANGCI_LINT_VERSION)
GOVULNCHECK_STAMP := $(BIN_DIR)/.govulncheck-$(GOVULNCHECK_VERSION)
GOLANGCI_LINT_CACHE := $(CURDIR)/.cache/golangci-lint
ABS_BIN_DIR := $(abspath $(BIN_DIR))

.PHONY: ci check fmt fmt-check tidy-check test vet build bin dogfood-no-token link-bin lint vuln race cover release-test toolchain-policy install-tools install-hooks gen-bootstrap-wrapper
.SILENT: fmt fmt-check link-bin

ci: build check

check: toolchain-policy fmt-check tidy-check test vet lint race cover vuln release-test

fmt:
	bash scripts/go-format.sh write

fmt-check:
	bash scripts/go-format.sh check

tidy-check:
	@set -e; \
	tmp="$$(mktemp -d)"; \
	trap 'rm -rf "$$tmp"' EXIT; \
	cp go.mod go.sum "$$tmp"/; \
	$(GO) mod tidy; \
	diff -u "$$tmp/go.mod" go.mod; \
	diff -u "$$tmp/go.sum" go.sum

test:
	"${GO}" test ./...

vet:
	"${GO}" vet ./...

build:
	"${GO}" build ./...

bin:
	"${GO}" build -o "./${BINARY}" ./cmd/serverpro

dogfood-no-token: bin
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
	"${GO}" test -cover ./...

release-test:
	GO="${GO}" bash scripts/test-release-package.sh
	bash scripts/test-release-workflow.sh

toolchain-policy:
	bash scripts/test-toolchain-policy.sh

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
