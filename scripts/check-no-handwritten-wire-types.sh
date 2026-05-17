#!/usr/bin/env bash
# check-no-handwritten-wire-types.sh
#
# Enforces hard-constraint #8 (CLAUDE.md): every wire-format type that crosses
# the gateway/SPA boundary must be defined in the contract specs and generated
# into pkg/api/generated/ (Go) or src/lib/api/generated/ (TypeScript). Hand-
# written wire-format structs and interfaces outside those directories are
# forbidden.
#
# Rules enforced:
#
#   GO: A named top-level struct in pkg/gateway/**/*.go (non-generated) that
#       (a) has >= 2 fields with `json:` tags AND
#       (b) whose type name ends in Frame, Response, Request, or Payload
#       is considered a hand-written wire-format type and is flagged.
#
#       Opt-out: add `// not-wire-format` on the same line as `type Foo struct {`
#                (case-insensitive) to suppress a false positive.
#
#   TS:  An `export interface` declaration in src/lib/**/*.ts (non-generated)
#        whose name ends in Frame or Response is flagged.
#
#        Re-export type aliases (export type FooFrame = ...) are allowed —
#        those are explicitly how ws.ts wraps generated types.
#
#        Opt-out: add `// not-wire-format` on the same line to suppress.
#
# Exit code: 0 if no offenders found, 1 if any found.
#
# Usage:
#   bash scripts/check-no-handwritten-wire-types.sh
#   bash scripts/check-no-handwritten-wire-types.sh --baseline   # suppress exit 1 (print findings only)
#
# Performance note: uses only grep/awk/python3 — runs in < 5 seconds on full repo.

set -uo pipefail

BASELINE_MODE=0
if [[ "${1:-}" == "--baseline" ]]; then
  BASELINE_MODE=1
fi

# Resolve repo root. The REPO_ROOT env variable overrides the default
# (script parent directory) — used by the self-test to point at a tmp fixture.
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="${REPO_ROOT:-$(cd "${SCRIPT_DIR}/.." && pwd)}"

FINDINGS=0
FINDING_LINES=()

# ─── Helper ───────────────────────────────────────────────────────────────────

emit() {
  local file="$1" line="$2" rule="$3" detail="$4"
  FINDING_LINES+=("${file}:${line}: [${rule}] ${detail}")
  FINDINGS=$((FINDINGS + 1))
}

# ─── Rule 1: Go — named structs with json tags ending in wire-type suffixes ──
#
# Algorithm (single Python pass for speed):
#   - Skip files under pkg/api/generated/
#   - For each .go file under pkg/gateway/
#   - Find lines matching `type <Name>(Frame|Response|Request|Payload) struct`
#   - That do NOT contain `// not-wire-format` (case-insensitive)
#   - Then scan the following lines until the closing `}` of the struct body
#   - Count fields with `json:"` tag
#   - If count >= 2, emit a finding

GO_OFFENDERS=$(python3 - "$REPO_ROOT" <<'PYEOF'
import re
import os
import sys

repo_root = sys.argv[1] if len(sys.argv) > 1 else '.'
gateway_dir = os.path.join(repo_root, 'pkg', 'gateway')
generated_dir = os.path.join(repo_root, 'pkg', 'api', 'generated')

WIRE_SUFFIXES = re.compile(r'type\s+\w*(Frame|Response|Request|Payload)\s+struct\s*\{', re.IGNORECASE)
NOT_WIRE_FORMAT = re.compile(r'//\s*not-wire-format', re.IGNORECASE)
JSON_TAG = re.compile(r'`[^`]*json:"[^"`]')

findings = []

if not os.path.isdir(gateway_dir):
    sys.exit(0)

for dirpath, dirnames, filenames in os.walk(gateway_dir):
    for fname in filenames:
        if not fname.endswith('.go'):
            continue
        fpath = os.path.join(dirpath, fname)
        # Skip generated files
        if os.path.commonpath([fpath, generated_dir]) == generated_dir:
            continue

        try:
            with open(fpath, 'r', encoding='utf-8', errors='replace') as f:
                lines = f.readlines()
        except OSError:
            continue

        i = 0
        while i < len(lines):
            line = lines[i]
            m = WIRE_SUFFIXES.search(line)
            if m:
                # Check opt-out marker on same line
                if NOT_WIRE_FORMAT.search(line):
                    i += 1
                    continue

                struct_start_line = i + 1  # 1-indexed
                type_name_m = re.search(r'type\s+(\w+)\s+struct', line)
                type_name = type_name_m.group(1) if type_name_m else '?'

                # Count json-tagged fields in struct body
                depth = 0
                json_count = 0
                j = i
                while j < len(lines):
                    l = lines[j]
                    depth += l.count('{') - l.count('}')
                    if j > i and JSON_TAG.search(l):
                        json_count += 1
                    if depth <= 0 and j > i:
                        break
                    j += 1

                if json_count >= 2:
                    relpath = os.path.relpath(fpath, repo_root)
                    findings.append(f"{relpath}:{struct_start_line}: [go-wire-type] hand-written wire-format struct '{type_name}' ({json_count} json fields) — migrate to contracts/components/schemas/ and regenerate")

            i += 1

for f in findings:
    print(f)
PYEOF
)

if [[ -n "$GO_OFFENDERS" ]]; then
  while IFS= read -r line; do
    FINDING_LINES+=("$line")
    FINDINGS=$((FINDINGS + 1))
  done <<< "$GO_OFFENDERS"
fi

# ─── Rule 2: TypeScript — export interface ending in Frame or Response ────────
#
# Only flag EXPORT INTERFACE declarations (not type aliases, not re-exports).
# Files under src/lib/api/generated/ are excluded.
# Opt-out: `// not-wire-format` on same line.

TS_LIB_DIR="${REPO_ROOT}/src/lib"

if [[ -d "$TS_LIB_DIR" ]]; then
  while IFS= read -r match; do
    # match format: path:linenum:content
    fpath="${match%%:*}"
    rest="${match#*:}"
    linenum="${rest%%:*}"
    content="${rest#*:}"

    # Skip generated directories
    if [[ "$fpath" == *"/generated/"* ]]; then
      continue
    fi

    # Skip opt-out marker
    if echo "$content" | grep -qi "not-wire-format"; then
      continue
    fi

    # Extract interface name
    iface_name=$(echo "$content" | grep -oP 'export\s+interface\s+\K\w+' || true)
    relpath="${fpath#$REPO_ROOT/}"
    emit "$relpath" "$linenum" "ts-wire-type" "hand-written wire-format interface '${iface_name}' — migrate to contracts/components/schemas/ and regenerate"

  done < <(grep -rn --include="*.ts" --include="*.tsx" \
    'export[[:space:]]\+interface[[:space:]]\+[A-Za-z]*\(Frame\|Response\)[[:space:]\n{]' \
    "$TS_LIB_DIR" 2>/dev/null | grep -v '/generated/' || true)
fi

# ─── Output ───────────────────────────────────────────────────────────────────

if [[ ${#FINDING_LINES[@]} -eq 0 ]]; then
  echo "check-no-handwritten-wire-types: OK (0 findings)"
  exit 0
fi

echo "check-no-handwritten-wire-types: ${#FINDING_LINES[@]} finding(s)"
echo ""
for line in "${FINDING_LINES[@]}"; do
  echo "  $line"
done
echo ""
echo "To suppress a false positive, add '// not-wire-format' on the same line"
echo "as the type/interface declaration."
echo ""
echo "To fix a real finding:"
echo "  1. Add the type to contracts/components/schemas/<TypeName>.yaml"
echo "  2. Reference it from contracts/openapi.yaml or contracts/asyncapi.yaml"
echo "  3. Run: make gen-contracts"
echo "  4. Commit the regenerated diff alongside the spec change"
echo "  5. Delete the hand-written struct/interface"

if [[ "$BASELINE_MODE" -eq 1 ]]; then
  exit 0
fi
exit 1
