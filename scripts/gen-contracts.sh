#!/usr/bin/env bash
# gen-contracts.sh — Single source-of-truth codegen for all wire-format types.
#
# Drives both _gen-ts.sh (Agent A — openapi-typescript, openapi-zod-client,
# custom AsyncAPI converter) and _gen-go.sh equivalent commands (Agent B —
# oapi-codegen + custom AsyncAPI Go converter).
#
# Idempotent: running twice in a clean tree produces no git diff.
# Used by `make gen-contracts` and `make verify-contracts` (the latter adds a
# git-diff-exit-code gate on top).
#
# Required tools (verified at top, fails fast if missing):
#   - npx + node_modules (openapi-typescript, openapi-zod-client, js-yaml, @asyncapi/parser, @redocly/cli)
#   - /usr/local/go/bin/go (or `go` in PATH)
#   - /home/Daniel/go/bin/oapi-codegen (or `oapi-codegen` in PATH — install via
#       `GOBIN=/home/Daniel/go/bin go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.7.0`)
#   - /usr/local/go/bin/gofmt (or `gofmt` in PATH)

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${REPO_ROOT}"

# Make sure Go toolchain is on PATH for child processes (oapi-codegen, gofmt)
if [ -d /usr/local/go/bin ] && ! echo "$PATH" | grep -q "/usr/local/go/bin"; then
  export PATH="/usr/local/go/bin:$PATH"
fi
if [ -d /home/Daniel/go/bin ] && ! echo "$PATH" | grep -q "/home/Daniel/go/bin"; then
  export PATH="/home/Daniel/go/bin:$PATH"
fi

echo "[gen-contracts] Working directory: ${REPO_ROOT}"

# ---------------------------------------------------------------------------
# Step 1: Lint specs
# ---------------------------------------------------------------------------
echo "[gen-contracts] Step 1/4: Linting contracts/openapi.yaml..."
npx --no-install @redocly/cli lint contracts/openapi.yaml --skip-rule no-server-example.com

echo "[gen-contracts] Step 1/4: Validating contracts/asyncapi.yaml..."
node -e "
  const { Parser } = require('@asyncapi/parser');
  const fs = require('fs');
  const p = new Parser();
  p.parse(fs.readFileSync('contracts/asyncapi.yaml', 'utf8')).then(r => {
    const errors = r.diagnostics.filter(d => d.severity === 0);
    if (errors.length > 0) {
      console.error('asyncapi.yaml validation errors:');
      console.error(JSON.stringify(errors, null, 2));
      process.exit(1);
    }
    console.log('asyncapi.yaml valid');
  }).catch(err => { console.error(err); process.exit(1); });
"

# ---------------------------------------------------------------------------
# Step 2: TypeScript types + Zod (delegated to Agent A's idempotent helper)
# ---------------------------------------------------------------------------
echo "[gen-contracts] Step 2/4: Generating TypeScript types + Zod schemas..."
bash scripts/_gen-ts.sh

# ---------------------------------------------------------------------------
# Step 3: Go types — REST via oapi-codegen + WS via custom converter
# ---------------------------------------------------------------------------
echo "[gen-contracts] Step 3/4: Generating Go types from openapi.yaml..."
mkdir -p pkg/api/generated

if ! command -v oapi-codegen >/dev/null 2>&1; then
  echo "[gen-contracts] ERROR: oapi-codegen not in PATH. Install with:" >&2
  echo "  GOBIN=/home/Daniel/go/bin go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.7.0" >&2
  exit 1
fi
oapi-codegen -config pkg/api/generated/oapi-codegen-config.yaml contracts/openapi.yaml

echo "[gen-contracts] Step 3/4: Generating Go types from asyncapi.yaml..."
if [ ! -d scripts/gen-asyncapi-go ]; then
  echo "[gen-contracts] ERROR: scripts/gen-asyncapi-go/ missing — Agent B's AsyncAPI Go converter not committed." >&2
  exit 1
fi
CGO_ENABLED=0 go run ./scripts/gen-asyncapi-go/ \
  contracts/asyncapi.yaml \
  pkg/api/generated/asyncapi_types.gen.go

# ---------------------------------------------------------------------------
# Step 4: Format generated Go files (deterministic gofmt)
# ---------------------------------------------------------------------------
echo "[gen-contracts] Step 4/4: Formatting generated Go files..."
gofmt -w pkg/api/generated/

echo "[gen-contracts] Done. All contract artifacts are up to date."
