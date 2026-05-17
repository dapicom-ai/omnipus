#!/usr/bin/env bash
# _gen-go.sh — Regenerate Go types from contracts/
#
# This script is consumed by scripts/gen-contracts.sh (Agent C) to regenerate
# the two Go generated files under pkg/api/generated/.
#
# Usage (from repo root):
#   bash scripts/_gen-go.sh
#
# Prerequisites:
#   - oapi-codegen v2 installed:
#       GOBIN=/home/Daniel/go/bin go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@latest
#   - Go 1.21+ in PATH (needed by oapi-codegen for formatting)
#   - gopkg.in/yaml.v3 available in go.mod (already present)
#
# Exact commands (for Agent C to fold into gen-contracts.sh):
#
#   Step 1: oapi-codegen → openapi_types.gen.go
#     PATH="/usr/local/go/bin:$PATH" \
#     /home/Daniel/go/bin/oapi-codegen \
#       -config pkg/api/generated/oapi-codegen-config.yaml \
#       contracts/openapi.yaml
#
#   Step 2: custom converter → asyncapi_types.gen.go
#     PATH="/usr/local/go/bin:$PATH" CGO_ENABLED=0 \
#     go run ./scripts/gen-asyncapi-go/ \
#       contracts/asyncapi.yaml \
#       pkg/api/generated/asyncapi_types.gen.go

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

OAPI_CODEGEN="${GOBIN:-/home/Daniel/go/bin}/oapi-codegen"
GO="${GO:-/usr/local/go/bin/go}"

# Ensure oapi-codegen is available.
if [[ ! -x "$OAPI_CODEGEN" ]]; then
  echo "oapi-codegen not found at $OAPI_CODEGEN" >&2
  echo "Install with: GOBIN=/home/Daniel/go/bin go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@latest" >&2
  exit 1
fi

# Ensure Go is available.
if [[ ! -x "$GO" ]]; then
  echo "go not found at $GO" >&2
  exit 1
fi

echo "==> Generating openapi_types.gen.go via oapi-codegen..."
PATH="/usr/local/go/bin:$PATH" "$OAPI_CODEGEN" \
  -config pkg/api/generated/oapi-codegen-config.yaml \
  contracts/openapi.yaml

echo "==> Generating asyncapi_types.gen.go via custom converter..."
PATH="/usr/local/go/bin:$PATH" CGO_ENABLED=0 "$GO" run ./scripts/gen-asyncapi-go/ \
  contracts/asyncapi.yaml \
  pkg/api/generated/asyncapi_types.gen.go

echo "==> Go type generation complete."
echo "    pkg/api/generated/openapi_types.gen.go"
echo "    pkg/api/generated/asyncapi_types.gen.go"
