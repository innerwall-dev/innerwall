#!/usr/bin/env sh
# Installs the pinned CLI toolchain into GOBIN (default: $GOPATH/bin).
# Keep these versions in sync with .github/workflows/ci.yml so local
# regeneration matches the CI drift check. protoc plugins are not listed here:
# they are go.mod `tool` directives and run via `go tool`.
set -eu

BUF_VERSION="v1.72.0"
SQLC_VERSION="v1.31.1"
GOOSE_VERSION="v3.28.0"
GOLANGCI_LINT_VERSION="v2.5.0"

echo "installing buf ${BUF_VERSION}"
go install "github.com/bufbuild/buf/cmd/buf@${BUF_VERSION}"
echo "installing sqlc ${SQLC_VERSION}"
go install "github.com/sqlc-dev/sqlc/cmd/sqlc@${SQLC_VERSION}"
echo "installing goose ${GOOSE_VERSION}"
go install "github.com/pressly/goose/v3/cmd/goose@${GOOSE_VERSION}"
echo "installing golangci-lint ${GOLANGCI_LINT_VERSION}"
go install "github.com/golangci/golangci-lint/v2/cmd/golangci-lint@${GOLANGCI_LINT_VERSION}"
echo "done; make sure $(go env GOPATH)/bin is on PATH"
