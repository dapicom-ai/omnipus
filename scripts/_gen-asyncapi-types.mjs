/**
 * _gen-asyncapi-types.mjs
 *
 * Generates src/lib/api/generated/asyncapi-types.ts from contracts/asyncapi.yaml.
 *
 * Approach: parse the AsyncAPI YAML, extract components.schemas, convert each
 * JSON Schema to a TypeScript interface/type using a purpose-built converter.
 * This is intentionally minimal — AsyncAPI 3 codegen tooling is immature and
 * the schema set is small enough to convert deterministically.
 *
 * Run with: node scripts/_gen-asyncapi-types.mjs
 * (Node resolves node_modules from the project root.)
 */

import { readFileSync, writeFileSync, existsSync } from "fs";
import { resolve, dirname, join } from "path";
import { fileURLToPath } from "url";
import { createRequire } from "module";

const __dirname = dirname(fileURLToPath(import.meta.url));
const ROOT = resolve(__dirname, "..");

const require = createRequire(import.meta.url);

// Resolve js-yaml from the nearest node_modules (supports worktree layouts where
// node_modules live in the main repo root rather than the worktree root).
function resolveJsYaml() {
  // Try git to find the main repo root (works even from inside a worktree)
  try {
    const { execSync } = require("child_process");
    const gitCommonDir = execSync("git rev-parse --git-common-dir", {
      cwd: ROOT, encoding: "utf8"
    }).trim();
    // git-common-dir is something like /path/to/repo/.git
    const mainRepoRoot = dirname(gitCommonDir);
    const candidate = join(mainRepoRoot, "node_modules", "js-yaml", "index.js");
    if (existsSync(candidate)) return candidate;
  } catch (_) {
    // fall through to manual search
  }
  // Fallback: walk up from the script dir
  const candidates = [
    join(ROOT, "node_modules", "js-yaml", "index.js"),
    join(ROOT, "..", "node_modules", "js-yaml", "index.js"),
    join(ROOT, "..", "..", "node_modules", "js-yaml", "index.js"),
    join(ROOT, "..", "..", "..", "node_modules", "js-yaml", "index.js"),
  ];
  for (const p of candidates) {
    if (existsSync(p)) return p;
  }
  throw new Error("js-yaml not found — install it in node_modules");
}

const yaml = require(resolveJsYaml());

// The contracts directory is always in the main git repo root.
// In a worktree, --git-common-dir resolves to the main repo's .git dir.
function resolveContractsDir() {
  const { execSync } = require("child_process");
  try {
    const gitCommonDir = execSync("git rev-parse --git-common-dir", {
      cwd: ROOT, encoding: "utf8"
    }).trim();
    const mainRepoRoot = dirname(gitCommonDir);
    const contractsPath = join(mainRepoRoot, "contracts");
    if (existsSync(contractsPath)) return contractsPath;
  } catch (_) {
    // fall through
  }
  // Fallback: try known relative locations
  for (const rel of ["contracts", "../contracts", "../../contracts", "../../../contracts"]) {
    const p = resolve(ROOT, rel);
    if (existsSync(p)) return p;
  }
  throw new Error("contracts/ directory not found");
}

const CONTRACTS_DIR = resolveContractsDir();

const asyncapiPath = resolve(CONTRACTS_DIR, "asyncapi.yaml");
const outPath = resolve(ROOT, "src/lib/api/generated/asyncapi-types.ts");

const doc = yaml.load(readFileSync(asyncapiPath, "utf8"));
const schemas = doc.components?.schemas ?? {};

// ── JSON Schema → TypeScript converter ──────────────────────────────────────

/** Convert a single JSON Schema node to a TypeScript type string. */
function schemaToTs(schema, indent = 0, schemaName = "") {
  if (!schema) return "unknown";

  const pad = "  ".repeat(indent);
  const inner = "  ".repeat(indent + 1);

  // $ref to sibling schema — use the name directly
  if (schema.$ref) {
    const parts = schema.$ref.split("/");
    return parts[parts.length - 1];
  }

  // const literal
  if (schema.const !== undefined) {
    return JSON.stringify(schema.const);
  }

  // enum
  if (Array.isArray(schema.enum)) {
    return schema.enum.map((v) => JSON.stringify(v)).join(" | ");
  }

  // type-based
  const type = schema.type;

  if (type === "string") return "string";
  if (type === "number") return "number";
  if (type === "integer") return "number";
  if (type === "boolean") return "boolean";

  if (type === "array") {
    if (schema.items) {
      return `Array<${schemaToTs(schema.items, indent)}>`;
    }
    return "Array<unknown>";
  }

  if (type === "object" || schema.properties) {
    const required = new Set(schema.required ?? []);
    const props = schema.properties ?? {};
    const lines = [];
    for (const [key, propSchema] of Object.entries(props)) {
      const opt = required.has(key) ? "" : "?";
      const tsType = schemaToTs(propSchema, indent + 1);
      lines.push(`${inner}${key}${opt}: ${tsType};`);
    }
    if (schema.additionalProperties === true || (typeof schema.additionalProperties === "object" && schema.additionalProperties !== false)) {
      lines.push(`${inner}[key: string]: unknown;`);
    }
    if (lines.length === 0) {
      return "Record<string, unknown>";
    }
    return `{\n${lines.join("\n")}\n${pad}}`;
  }

  // empty schema (result: {}) — any JSON value
  if (!type && !schema.properties && !schema.items && !schema.$ref && !schema.enum && schema.const === undefined) {
    return "unknown";
  }

  return "unknown";
}

// ── Generate TypeScript output ───────────────────────────────────────────────

const lines = [
  "/**",
  " * This file was auto-generated from contracts/asyncapi.yaml.",
  " * Do not make direct changes to the file.",
  " * Re-run: node scripts/_gen-asyncapi-types.mjs",
  " */",
  "",
  "// ── WebSocket frame type discriminator ──────────────────────────────────────",
  "",
];

// Emit WsFrameType enum first
const wsFrameType = schemas["WsFrameType"];
if (wsFrameType?.enum) {
  lines.push("export type WsFrameType =");
  wsFrameType.enum.forEach((v, i) => {
    const sep = i === wsFrameType.enum.length - 1 ? ";" : " |";
    lines.push(`  | "${v}"`);
  });
  lines[lines.length - 1] = lines[lines.length - 1].replace(/^\s*\| /, "  | ") + ";";
  lines.push("");
}

lines.push("// ── Frame payload types ─────────────────────────────────────────────────────");
lines.push("");

// Emit all other schemas in definition order
const skipNames = new Set(["WsFrameType"]);
for (const [name, schema] of Object.entries(schemas)) {
  if (skipNames.has(name)) continue;

  const tsBody = schemaToTs(schema, 0, name);
  lines.push(`export interface ${name} ${tsBody}`);
  lines.push("");
}

// ── Union type of all frame types ────────────────────────────────────────────
// Collect names that are "frame" types (have a `type` discriminator property)
const frameNames = Object.keys(schemas).filter(
  (name) =>
    name !== "WsFrameType" &&
    schemas[name].properties?.type?.const !== undefined
);

lines.push(
  "// ── Union of all WS frames (discriminated by the `type` field) ──────────────"
);
lines.push("");
lines.push("export type WsFrame =");
frameNames.forEach((name, i) => {
  const sep = i === frameNames.length - 1 ? ";" : "";
  lines.push(`  | ${name}${sep}`);
});
lines.push("");

// ── Client→server frame union ─────────────────────────────────────────────────
const clientFrames = [
  "AuthFrame",
  "MessageFrame",
  "CancelFrame",
  "ExecApprovalResponseFrame",
  "PingFrame",
  "AttachSessionFrame",
  "DevicePairingResponseFrame",
];

lines.push("// ── Client → server frames ──────────────────────────────────────────────────");
lines.push("");
lines.push("export type ClientFrame =");
clientFrames.forEach((name, i) => {
  const sep = i === clientFrames.length - 1 ? ";" : "";
  lines.push(`  | ${name}${sep}`);
});
lines.push("");

// ── Server→client frame union ─────────────────────────────────────────────────
const serverFrames = frameNames.filter((n) => !clientFrames.includes(n));
lines.push("// ── Server → client frames ──────────────────────────────────────────────────");
lines.push("");
lines.push("export type ServerFrame =");
serverFrames.forEach((name, i) => {
  const sep = i === serverFrames.length - 1 ? ";" : "";
  lines.push(`  | ${name}${sep}`);
});
lines.push("");

const output = lines.join("\n");
writeFileSync(outPath, output, "utf8");
console.log(`Generated ${outPath} (${output.split("\n").length} lines)`);
