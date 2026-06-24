#!/usr/bin/env node
// Regenerate packages/engine/test/corpus/cases.json — the frozen conformance manifest.
//
// For every decision in every corpus/*.rules: sample an input from the decision's required-input
// metadata, run it through the native `feelc` CLI (the deterministic oracle), and record
// {file, decision, input, expected | error}. The conformance test replays these through WASM.
//
// Run after intentionally changing the engine's semantics or adding corpus files:
//   make build && npm -w @feelc/engine run build && node packages/engine/scripts/capture-cases.mjs
//
// Requires the native CLI: $FEELC_BIN, `feelc` on PATH, or ../../../feelc (repo root).
import { execFileSync } from "node:child_process";
import { existsSync, readdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const pkg = join(here, "..");
const repoRoot = join(pkg, "..", "..");
const CLI = process.env.FEELC_BIN || join(repoRoot, "feelc");
const CORPUS = join(pkg, "test", "corpus");
if (!existsSync(CLI)) {
  console.error(`capture-cases: native feelc not found at ${CLI} (set FEELC_BIN or run \`make build\`)`);
  process.exit(1);
}

const { createEngine } = await import(join(pkg, "dist", "index.js"));
const feelc = await createEngine();

function sampleValue(info) {
  switch (info.type) {
    case "boolean": return true;
    case "date": return "2024-06-01";
    case "duration": return "P1D";
    case "string": {
      const m = /in \{(.+)\}/.exec(info.domain || "");
      if (m) return m[1].split(",")[0].trim().replace(/^"|"$/g, "");
      return "x";
    }
    case "number": {
      const m = /in [[(]\s*(\S+?)\s*\.\.\s*(\S+?)\s*[\])]/.exec(info.domain || "");
      if (m) { const [, lo, hi] = m; if (!lo.includes("inf")) return Number(lo); if (!hi.includes("inf")) return Number(hi) - 1; }
      return 1;
    }
    default: return undefined;
  }
}
function cliRun(file, decision, input) {
  try {
    const out = execFileSync(CLI, ["run", "--rules", file, "--decision", decision, "--input", JSON.stringify(input), "--json"], { encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] });
    return { ok: true, value: JSON.parse(out).output };
  } catch { return { ok: false }; }
}

const cases = [];
let skipped = 0;
for (const f of readdirSync(CORPUS).filter((x) => x.endsWith(".rules")).sort()) {
  const path = join(CORPUS, f);
  const src = readFileSync(path, "utf8");
  for (const dec of feelc.model(src).decisions) {
    const required = feelc.required(src, dec.name).inputs;
    const sampled = required.map(sampleValue);
    if (sampled.some((v) => v === undefined)) { skipped++; continue; }
    const input = Object.fromEntries(required.map((info, i) => [info.name, sampled[i]]));
    const cli = cliRun(path, dec.name, input);
    cases.push({ file: f, decision: dec.name, input, ...(cli.ok ? { expected: cli.value } : { error: true }) });
  }
}
writeFileSync(join(CORPUS, "cases.json"), JSON.stringify(cases, null, 2) + "\n");
console.log(`capture-cases: wrote ${cases.length} cases (${cases.filter((c) => c.error).length} expected-error, ${skipped} skipped)`);
