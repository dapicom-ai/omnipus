#!/usr/bin/env bash
# check-no-handwritten-wire-types.test.sh
#
# Self-test for check-no-handwritten-wire-types.sh.
#
# Creates temporary fixture files under /tmp/ that simulate:
#   1. A hand-written Go wire-format struct (should be CAUGHT → exit 1)
#   2. A hand-written TS wire-format interface (should be CAUGHT → exit 1)
#   3. A Go struct with `// not-wire-format` opt-out (should be SKIPPED → exit 0)
#   4. A TS interface with `// not-wire-format` opt-out (should be SKIPPED → exit 0)
#   5. A Go generated file (should be SKIPPED → exit 0)
#
# Exit code: 0 if all assertions pass, 1 if any assertion fails.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LINT_SCRIPT="${SCRIPT_DIR}/check-no-handwritten-wire-types.sh"

PASS=0
FAIL=0
ERRORS=()

# ─── Fixture helpers ──────────────────────────────────────────────────────────

TMP_DIR=$(mktemp -d /tmp/wire-lint-test.XXXXXX)
trap 'rm -rf "$TMP_DIR"' EXIT

setup_go_fixture() {
  local subpath="$1"
  local content="$2"
  local fpath="${TMP_DIR}/${subpath}"
  mkdir -p "$(dirname "$fpath")"
  printf '%s\n' "$content" > "$fpath"
}

setup_ts_fixture() {
  local subpath="$1"
  local content="$2"
  local fpath="${TMP_DIR}/${subpath}"
  mkdir -p "$(dirname "$fpath")"
  printf '%s\n' "$content" > "$fpath"
}

# ─── Assert helpers ───────────────────────────────────────────────────────────

assert_exit_code() {
  local label="$1"
  local expected="$2"
  local actual="$3"
  if [[ "$actual" -eq "$expected" ]]; then
    echo "  PASS [$label]: exit $actual (expected $expected)"
    PASS=$((PASS + 1))
  else
    echo "  FAIL [$label]: exit $actual (expected $expected)"
    FAIL=$((FAIL + 1))
    ERRORS+=("[$label] expected exit $expected, got $actual")
  fi
}

assert_output_contains() {
  local label="$1"
  local needle="$2"
  local haystack="$3"
  if echo "$haystack" | grep -q "$needle"; then
    echo "  PASS [$label]: output contains '$needle'"
    PASS=$((PASS + 1))
  else
    echo "  FAIL [$label]: output does NOT contain '$needle'"
    FAIL=$((FAIL + 1))
    ERRORS+=("[$label] expected output to contain '$needle'")
  fi
}

assert_output_not_contains() {
  local label="$1"
  local needle="$2"
  local haystack="$3"
  if echo "$haystack" | grep -q "$needle"; then
    echo "  FAIL [$label]: output unexpectedly contains '$needle'"
    FAIL=$((FAIL + 1))
    ERRORS+=("[$label] expected output to NOT contain '$needle'")
  else
    echo "  PASS [$label]: output does not contain '$needle'"
    PASS=$((PASS + 1))
  fi
}

echo "=== check-no-handwritten-wire-types self-test ==="
echo ""

# ─── Test 1: Go fixture — hand-written Response struct (should be caught) ─────

echo "Test 1: Go hand-written wire-format struct is caught"

setup_go_fixture "pkg/gateway/fixture_wire_test.go" '
package gateway

// FooResponse is a hand-written wire-format struct — should be flagged.
type FooResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	OK   bool   `json:"ok"`
}
'

OUTPUT=$(REPO_ROOT="$TMP_DIR" bash "$LINT_SCRIPT" --baseline 2>&1)
EXIT_CODE=$?

assert_exit_code "go-caught-exit" 0 "$EXIT_CODE"
assert_output_contains "go-caught-finding" "FooResponse" "$OUTPUT"
assert_output_contains "go-caught-rule" "go-wire-type" "$OUTPUT"

# ─── Test 2: Go fixture — struct with opt-out comment (should be skipped) ─────

echo ""
echo "Test 2: Go struct with not-wire-format comment is skipped"

# Remove previous fixture and replace with opt-out version
setup_go_fixture "pkg/gateway/fixture_wire_test.go" '
package gateway

// BarResponse has the opt-out marker — should NOT be flagged.
type BarResponse struct { // not-wire-format
	ID   string `json:"id"`
	Name string `json:"name"`
	OK   bool   `json:"ok"`
}
'

OUTPUT=$(REPO_ROOT="$TMP_DIR" bash "$LINT_SCRIPT" --baseline 2>&1)
EXIT_CODE=$?

assert_exit_code "go-skipout-exit" 0 "$EXIT_CODE"
assert_output_not_contains "go-skipout-not-flagged" "BarResponse" "$OUTPUT"

# ─── Test 3: Go fixture — struct in generated directory (should be skipped) ───

echo ""
echo "Test 3: Go struct in generated directory is skipped"

# Put a hand-written-looking struct in the generated dir — must be skipped
setup_go_fixture "pkg/api/generated/baz_gen.go" '
package generated

type BazResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Val  int    `json:"val"`
}
'
# And ensure no gateway fixture triggers
setup_go_fixture "pkg/gateway/fixture_wire_test.go" '
package gateway
// empty
'

OUTPUT=$(REPO_ROOT="$TMP_DIR" bash "$LINT_SCRIPT" --baseline 2>&1)
EXIT_CODE=$?

assert_exit_code "go-generated-skip-exit" 0 "$EXIT_CODE"
assert_output_not_contains "go-generated-skip-not-flagged" "BazResponse" "$OUTPUT"

# ─── Test 4: TS fixture — export interface FooFrame (should be caught) ────────

echo ""
echo "Test 4: TS hand-written interface export is caught"

setup_ts_fixture "src/lib/ws_fixture.ts" '
// Hand-written wire-format interface — should be flagged.
export interface FooFrame {
  type: string
  payload: unknown
}
'

# Still no gateway Go file triggering
setup_go_fixture "pkg/gateway/fixture_wire_test.go" '
package gateway
// empty
'

OUTPUT=$(REPO_ROOT="$TMP_DIR" bash "$LINT_SCRIPT" --baseline 2>&1)
EXIT_CODE=$?

assert_exit_code "ts-caught-exit" 0 "$EXIT_CODE"
assert_output_contains "ts-caught-finding" "FooFrame" "$OUTPUT"
assert_output_contains "ts-caught-rule" "ts-wire-type" "$OUTPUT"

# ─── Test 5: TS fixture — interface with opt-out (should be skipped) ──────────

echo ""
echo "Test 5: TS interface with not-wire-format comment is skipped"

setup_ts_fixture "src/lib/ws_fixture.ts" '
// This internal helper looks like a frame but is not a wire type.
export interface BarFrame { // not-wire-format
  type: string
  payload: unknown
}
'

OUTPUT=$(REPO_ROOT="$TMP_DIR" bash "$LINT_SCRIPT" --baseline 2>&1)
EXIT_CODE=$?

assert_exit_code "ts-skipout-exit" 0 "$EXIT_CODE"
assert_output_not_contains "ts-skipout-not-flagged" "BarFrame" "$OUTPUT"

# ─── Test 6: TS fixture — interface in generated directory (should be skipped)

echo ""
echo "Test 6: TS interface in generated directory is skipped"

setup_ts_fixture "src/lib/api/generated/asyncapi-types.ts" '
// Generated — should NOT be flagged.
export interface GenFrame {
  type: string
}
'
setup_ts_fixture "src/lib/ws_fixture.ts" '
// no hand-written frames here
'

OUTPUT=$(REPO_ROOT="$TMP_DIR" bash "$LINT_SCRIPT" --baseline 2>&1)
EXIT_CODE=$?

assert_exit_code "ts-generated-skip-exit" 0 "$EXIT_CODE"
assert_output_not_contains "ts-generated-skip-not-flagged" "GenFrame" "$OUTPUT"

# ─── Test 7: Verify script exits 1 when findings exist (not --baseline) ───────

echo ""
echo "Test 7: Script exits 1 when findings exist (non-baseline mode)"

setup_go_fixture "pkg/gateway/fixture_wire_test.go" '
package gateway

type CatchMeResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Err  string `json:"error"`
}
'

bash "$LINT_SCRIPT" -- "$TMP_DIR" >/dev/null 2>&1
EXIT_CODE_DIRECT=$?

# Use REPO_ROOT env approach
REPO_ROOT="$TMP_DIR" bash "$LINT_SCRIPT" >/dev/null 2>&1
EXIT_CODE_ENV=$?

assert_exit_code "nonbaseline-exit-is-1" 1 "$EXIT_CODE_ENV"

# ─── Summary ──────────────────────────────────────────────────────────────────

echo ""
echo "─────────────────────────────────────────"
echo "Results: ${PASS} passed, ${FAIL} failed"

if [[ "$FAIL" -gt 0 ]]; then
  echo ""
  echo "Failures:"
  for e in "${ERRORS[@]}"; do
    echo "  - $e"
  done
  exit 1
fi

echo "All assertions passed."
exit 0
