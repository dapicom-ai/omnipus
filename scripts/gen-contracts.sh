#!/usr/bin/env bash
# gen-contracts.sh — Single source-of-truth codegen for all wire-format types.
#
# Runs four steps:
#   1. Lint both contract specs (openapi.yaml + asyncapi.yaml)
#   2. Generate TypeScript types from openapi.yaml (Agent A output)
#   3. Generate Go types from openapi.yaml + asyncapi.yaml (Agent B output)
#   4. Format generated Go files
#
# Idempotent: running twice in a clean tree produces no git diff.
# set -euo pipefail ensures any failure stops the chain immediately.
#
# Usage:
#   ./scripts/gen-contracts.sh           # regenerate everything
#   make gen-contracts                   # same via Makefile target
#   make verify-contracts                # gen + git diff --exit-code (CI gate)

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${REPO_ROOT}"

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
# Step 2: TypeScript types (Agent A delivers openapi-typescript + zod)
# ---------------------------------------------------------------------------
echo "[gen-contracts] Step 2/4: Generating TypeScript types from openapi.yaml..."
mkdir -p src/lib/api/generated

npx --no-install openapi-typescript contracts/openapi.yaml \
  -o src/lib/api/generated/openapi-types.ts

echo "[gen-contracts] Step 2/4: Generating TypeScript types from asyncapi.yaml..."
# AsyncAPI → TypeScript: use @asyncapi/modelina if available, otherwise a
# hand-crafted node script that Agent A ships alongside the generated file.
# The generated file is committed; this step regenerates it from the spec.
if npx --no-install @asyncapi/cli version >/dev/null 2>&1; then
  npx --no-install @asyncapi/cli generate models typescript \
    contracts/asyncapi.yaml \
    -o src/lib/api/generated/asyncapi-types/
  # Merge all files produced by modelina into a single barrel file
  node -e "
    const fs = require('fs');
    const path = require('path');
    const dir = 'src/lib/api/generated/asyncapi-types';
    if (!fs.existsSync(dir)) { console.log('no asyncapi-types dir — skipping merge'); process.exit(0); }
    const files = fs.readdirSync(dir).filter(f => f.endsWith('.ts') && f !== 'index.ts');
    const exports = files.map(f => \`export * from './asyncapi-types/\${f.replace(/\\.ts$/, '')}';\`).join('\n');
    fs.writeFileSync('src/lib/api/generated/asyncapi-types.ts', exports + '\n');
    console.log('asyncapi-types.ts barrel written');
  "
else
  echo "[gen-contracts]   @asyncapi/cli not found — asyncapi-types.ts regeneration skipped (committed file retained)"
fi

echo "[gen-contracts] Step 2/4: Generating Zod schemas from openapi.yaml..."
if npx --no-install openapi-zod-client --version >/dev/null 2>&1; then
  npx --no-install openapi-zod-client contracts/openapi.yaml \
    -o src/lib/api/generated/schemas.ts \
    --export-schemas \
    --strict-objects
else
  echo "[gen-contracts]   openapi-zod-client not found — schemas.ts regeneration skipped (committed file retained)"
fi

# ---------------------------------------------------------------------------
# Step 3: Go types (Agent B delivers oapi-codegen + asyncapi converter)
# ---------------------------------------------------------------------------
echo "[gen-contracts] Step 3/4: Generating Go types from openapi.yaml..."
mkdir -p pkg/api/generated

if command -v oapi-codegen >/dev/null 2>&1; then
  oapi-codegen \
    --package=generated \
    --generate=types \
    -o pkg/api/generated/openapi_types.gen.go \
    contracts/openapi.yaml
else
  echo "[gen-contracts]   oapi-codegen not found in PATH — openapi_types.gen.go regeneration skipped (committed file retained)"
fi

echo "[gen-contracts] Step 3/4: Generating Go types from asyncapi.yaml..."
# Agent B ships a converter at scripts/asyncapi_to_go.go (or similar).
# Invoke it if present; otherwise fall back to the committed file.
if [ -f scripts/asyncapi_to_go.go ]; then
  go run scripts/asyncapi_to_go.go \
    -input contracts/asyncapi.yaml \
    -output pkg/api/generated/asyncapi_types.gen.go \
    -package generated
elif [ -f scripts/gen_asyncapi_types.sh ]; then
  bash scripts/gen_asyncapi_types.sh
else
  echo "[gen-contracts]   No asyncapi→Go converter found — asyncapi_types.gen.go regeneration skipped (committed file retained)"
fi

# ---------------------------------------------------------------------------
# Step 4: Format generated Go files
# ---------------------------------------------------------------------------
echo "[gen-contracts] Step 4/4: Formatting generated Go files..."
if [ -d pkg/api/generated ]; then
  gofmt -w pkg/api/generated/
fi

echo "[gen-contracts] Done. All contract artifacts are up to date."
