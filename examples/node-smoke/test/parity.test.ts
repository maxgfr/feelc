// Drift guard: the WASM engine (feelc) must produce byte-identical results to the native
// `feelc` CLI, and the .ir.bin artifact must be interchangeable between the two tools.
//
// Prereqs (handled by CI, see .github/workflows/npm.yml):
//   - the native CLI is built at the repo root (`make build` -> ./feelc)
//   - feelc is built (`npm -w feelc run build`)
import { execFileSync } from "node:child_process";
import { mkdtempSync, readdirSync, readFileSync, statSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";
import { createEngine, type InputInfo, type InputValue } from "feelc";

const here = fileURLToPath(new URL(".", import.meta.url));
const repoRoot = resolve(here, "../../.."); // test/ -> node-smoke/ -> examples/ -> repo root
const CLI = join(repoRoot, "feelc");

/** Run a decision through the native CLI and return its `output`. */
function cliRun(rulesPath: string, decision: string, input: Record<string, unknown>): unknown {
  const stdout = execFileSync(
    CLI,
    ["run", "--rules", rulesPath, "--decision", decision, "--input", JSON.stringify(input), "--json"],
    { encoding: "utf8" },
  );
  return JSON.parse(stdout).output;
}

interface Case {
  file: string;
  decision: string;
  input: Record<string, unknown>;
  expect: unknown;
}

// Curated cases with known-good inputs across different model shapes (collect-max table, FEEL
// expression decision, context-output table with FIRST hit policy).
const CASES: Case[] = [
  {
    file: "examples/promo/promo.rules",
    decision: "discount_pct",
    input: { cart_total: 120, is_member: true, promo_code: "BIG20" },
    expect: 20,
  },
  {
    file: "examples/promo/promo.rules",
    decision: "discount_pct",
    input: { cart_total: 10, is_member: false, promo_code: "none" },
    expect: null,
  },
  {
    file: "examples/credit/credit.rules",
    decision: "dti",
    input: { annual_income: 60000, monthly_debt: 1000 },
    expect: 0.2,
  },
  {
    file: "examples/credit/credit.rules",
    decision: "eligibility",
    input: { credit_score: 700, annual_income: 60000, monthly_debt: 1000, age: 30 },
    expect: { eligible: true, reason: "approved" },
  },
  {
    file: "examples/credit/credit.rules",
    decision: "eligibility",
    input: { credit_score: 500, annual_income: 60000, monthly_debt: 1000, age: 30 },
    expect: { eligible: false, reason: "insufficient score" },
  },
];

// Top-level await: the engine must be ready before the sweep below enumerates decisions during
// test collection (vitest supports top-level await in test files).
const feelc = await createEngine();

describe("WASM ⇄ native CLI parity", () => {
  for (const c of CASES) {
    it(`${c.file} · ${c.decision} · ${JSON.stringify(c.input)}`, () => {
      const path = join(repoRoot, c.file);
      const source = readFileSync(path, "utf8");

      const wasmRun = feelc.run(source, c.decision, c.input).output;
      const wasmCompiled = feelc.compile(source);
      const wasmEval = wasmCompiled.evaluate(c.decision, c.input).output;
      wasmCompiled.dispose();
      const cli = cliRun(path, c.decision, c.input);

      expect(wasmRun).toEqual(c.expect); // sanity: the curated expectation holds
      expect(wasmRun).toEqual(cli); // parity: run() matches the CLI
      expect(wasmEval).toEqual(cli); // parity: compile()+evaluate() matches the CLI
    });
  }
});

describe(".ir.bin is interchangeable between WASM and the CLI", () => {
  const tmp = mkdtempSync(join(tmpdir(), "feelc-parity-"));
  const promo = join(repoRoot, "examples/promo/promo.rules");
  const input = { cart_total: 120, is_member: true, promo_code: "BIG20" };

  it("loads a CLI-compiled .ir.bin in WASM", () => {
    const irPath = join(tmp, "cli.ir.bin");
    execFileSync(CLI, ["compile", "--rules", promo, "-o", irPath]);
    const bytes = readFileSync(irPath);
    const model = feelc.load(bytes);
    expect(model.evaluate("discount_pct", input).output).toEqual(cliRun(promo, "discount_pct", input));
    model.dispose();
  });

  it("runs a WASM-exported .ir.bin in the CLI", () => {
    const compiled = feelc.compile(readFileSync(promo, "utf8"));
    const irPath = join(tmp, "wasm.ir.bin");
    writeFileSync(irPath, compiled.export());
    compiled.dispose();
    expect(cliRun(irPath, "discount_pct", input)).toEqual(20);
  });
});

// Auto-discovery sweep: every decision in every example must produce identical results in the WASM
// engine and the native CLI. Inputs are sampled from each decision's required-input metadata, so this
// exercises the full DSL surface (tables, hit policies, FEEL expressions, context outputs, units,
// dates, durations, brackets, applicability) without hand-curating each case.
function sampleValue(info: InputInfo): InputValue | undefined {
  switch (info.type) {
    case "boolean":
      return true;
    case "date":
      return "2024-06-01";
    case "duration":
      return "P1D";
    case "string": {
      const enumMatch = /in \{(.+)\}/.exec(info.domain ?? "");
      if (enumMatch) return enumMatch[1].split(",")[0].trim().replace(/^"|"$/g, "");
      return "x";
    }
    case "number": {
      const range = /in [[(]\s*(\S+?)\s*\.\.\s*(\S+?)\s*[\])]/.exec(info.domain ?? "");
      if (range) {
        const [, lo, hi] = range;
        if (!lo.includes("inf")) return Number(lo);
        if (!hi.includes("inf")) return Number(hi) - 1;
      }
      return 1;
    }
    default:
      return undefined; // context-typed input we can't auto-build → skip the decision
  }
}

function cliResult(file: string, decision: string, input: Record<string, unknown>): { ok: boolean; value?: unknown } {
  try {
    const stdout = execFileSync(
      CLI,
      ["run", "--rules", file, "--decision", decision, "--input", JSON.stringify(input), "--json"],
      { encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] },
    );
    return { ok: true, value: JSON.parse(stdout).output };
  } catch {
    return { ok: false };
  }
}

function exampleRulesFiles(): string[] {
  const root = join(repoRoot, "examples");
  return readdirSync(root)
    .map((d) => join(root, d))
    .filter((p) => statSync(p).isDirectory())
    .flatMap((dir) => readdirSync(dir).filter((f) => f.endsWith(".rules")).map((f) => join(dir, f)));
}

function corpusRulesFiles(): string[] {
  const dir = join(repoRoot, "packages/engine/test/corpus");
  return readdirSync(dir)
    .filter((f) => f.endsWith(".rules"))
    .map((f) => join(dir, f));
}

describe("full DSL sweep: every example + corpus decision matches the CLI", () => {
  for (const file of [...exampleRulesFiles(), ...corpusRulesFiles()]) {
    const rel = file.replace(`${repoRoot}/`, "");
    const source = readFileSync(file, "utf8");
    let decisions: { name: string }[];
    try {
      decisions = feelc.model(source).decisions;
    } catch (e) {
      it.skip(`${rel} · (does not compile: ${(e as Error).message.split("\n")[0]})`, () => {});
      continue;
    }
    for (const dec of decisions) {
      it(`${rel} · ${dec.name}`, () => {
        const required = feelc.required(source, dec.name).inputs;
        const sampled = required.map(sampleValue);
        if (sampled.some((v) => v === undefined)) return; // unsupported input type → skip
        const input = Object.fromEntries(required.map((info, i) => [info.name, sampled[i]]));

        let wasm: { ok: boolean; value?: unknown };
        try {
          wasm = { ok: true, value: feelc.run(source, dec.name, input).output };
        } catch {
          wasm = { ok: false };
        }
        const cli = cliResult(file, dec.name, input);

        expect(wasm.ok).toBe(cli.ok); // both accept or both reject the same input
        if (wasm.ok && cli.ok) expect(wasm.value).toEqual(cli.value);
      });
    }
  }
});
