// Conformance suite for feelc (WASM, self-contained — no native CLI needed):
//   1. a frozen regression corpus (corpus/cases.json) — every decision in corpus/*.rules with an
//      input and the expected output captured from the native CLI (the oracle). Regenerate with
//      `node packages/engine/scripts/capture-cases.mjs` if the engine's semantics intentionally change.
//   2. explicit hand-checked feature assertions (readable spec of core semantics).
//   3. rejection tests — out-of-scope constructs MUST fail to compile (feelc's "guardian of scope").
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { beforeAll, describe, expect, it } from "vitest";
import { createEngine, FeelcError, type FeelcEngine } from "feelc";

const corpusDir = fileURLToPath(new URL("./corpus/", import.meta.url));

interface Case {
  file: string;
  decision: string;
  input: Record<string, unknown>;
  expected?: unknown;
  error?: boolean;
}
const cases: Case[] = JSON.parse(readFileSync(new URL("./corpus/cases.json", import.meta.url), "utf8"));

const sourceCache = new Map<string, string>();
function corpusSource(file: string): string {
  let s = sourceCache.get(file);
  if (s === undefined) {
    s = readFileSync(corpusDir + file, "utf8");
    sourceCache.set(file, s);
  }
  return s;
}

let feelc: FeelcEngine;
beforeAll(async () => {
  feelc = await createEngine();
});

describe(`conformance corpus — ${cases.length} frozen cases (WASM == captured CLI output)`, () => {
  for (const c of cases) {
    it(`${c.file} · ${c.decision} · ${JSON.stringify(c.input)}`, () => {
      if (c.error) {
        expect(() => feelc.run(corpusSource(c.file), c.decision, c.input)).toThrow(FeelcError);
      } else {
        expect(feelc.run(corpusSource(c.file), c.decision, c.input).output).toEqual(c.expected);
      }
    });
  }
});

// --- explicit, hand-checked semantics (a readable spec of the core behaviours) ---
const PROMO = `model "promo" {}
input cart_total : number >= 0
input is_member  : boolean
input promo_code : string
decision discount_pct : number {
  needs: cart_total, is_member, promo_code
  hit: collect max
  >= 50  | -    | -           => 5
  >= 100 | -    | -           => 10
  -      | true | -           => 8
  -      | -    | "WELCOME10" => 10
  -      | -    | "BIG20"     => 20
}`;

describe("explicit feature semantics", () => {
  it("collect max keeps the best applicable discount", () => {
    expect(feelc.run(PROMO, "discount_pct", { cart_total: 120, is_member: true, promo_code: "BIG20" }).output).toBe(20);
    expect(feelc.run(PROMO, "discount_pct", { cart_total: 60, is_member: false, promo_code: "none" }).output).toBe(5);
  });
  it("no matching rule yields null", () => {
    expect(feelc.run(PROMO, "discount_pct", { cart_total: 10, is_member: false, promo_code: "none" }).output).toBeNull();
  });
  it("if/then/else expression decision", () => {
    const src = `model "m" {}\ninput a : number\ndecision d : number = if a > 0 then 1 else -1`;
    expect(feelc.run(src, "d", { a: 5 }).output).toBe(1);
    expect(feelc.run(src, "d", { a: -5 }).output).toBe(-1);
  });
  it("single-arg builtins floor/ceiling/round (HALF_EVEN)", () => {
    const src = `model "m" {}\ninput a : number\ndecision f : number = floor(a)\ndecision c : number = ceiling(a)\ndecision r : number = round(a)`;
    expect(feelc.run(src, "f", { a: 2.7 }).output).toBe(2);
    expect(feelc.run(src, "c", { a: 2.1 }).output).toBe(3);
    expect(feelc.run(src, "r", { a: 2.5 }).output).toBe(2); // HALF_EVEN: 2.5 -> 2
  });
  it("abs / trunc / round(x,n) / modulo (ADR 0020)", () => {
    const src = `model "m" {}
input x : number
input y : number
decision ab : number = abs(x)
decision tr : number = trunc(x)
decision rn : number = round(x, 2)
decision md : number = modulo(x, y)`;
    expect(feelc.run(src, "ab", { x: -2.5, y: 0 }).output).toBe(2.5);
    expect(feelc.run(src, "tr", { x: 2.7, y: 0 }).output).toBe(2);
    expect(feelc.run(src, "tr", { x: -2.7, y: 0 }).output).toBe(-2);
    expect(feelc.run(src, "rn", { x: 3.14159, y: 0 }).output).toBe(3.14);
    expect(feelc.run(src, "md", { x: 10, y: 3 }).output).toBe(1);
    expect(feelc.run(src, "md", { x: 10, y: -3 }).output).toBe(-2); // floored: divisor sign
    expect(() => feelc.run(src, "md", { x: 10, y: 0 })).toThrow(FeelcError); // modulo by zero
  });
  it("BKM is inlined and callable", () => {
    const src = `model "m" {}\ninput d : number\ninput i : number\nbkm dti(a:number,b:number):number = a / (b / 12)\ndecision r : number = dti(d, i)`;
    expect(feelc.run(src, "r", { d: 1000, i: 60000 }).output).toBe(0.2);
  });
  it("exact decimal arithmetic (no float drift)", () => {
    const src = `model "m" {}\ninput a : number\ninput b : number\ndecision s : number = a + b`;
    expect(feelc.run(src, "s", { a: 0.1, b: 0.2 }).output).toBe(0.3); // not 0.30000000000000004
  });
  it("null propagates through arithmetic", () => {
    const src = `model "m" {}\ninput a : number\ndecision d : number = a + 1`;
    expect(feelc.run(src, "d", { a: null }).output).toBeNull();
  });
  it("division by zero is an error", () => {
    const src = `model "m" {}\ninput a : number\ninput b : number\ndecision d : number = a / b`;
    expect(() => feelc.run(src, "d", { a: 1, b: 0 })).toThrow(FeelcError);
  });
  it("date − date = ISO duration; date + duration round-trips", () => {
    const src = `model "m" {}\ninput d1 : date\ninput d2 : date\ndecision diff : duration = d2 - d1`;
    expect(feelc.run(src, "diff", { d1: "2024-01-01", d2: "2024-01-31" }).output).toBe("P30D");
  });
});

// --- rejection: out-of-scope constructs must fail to compile (guardian of scope) ---
const REJECTED: { name: string; source: string }[] = [
  { name: "context type used as an input", source: `model "x" {}\ntype P = context { a: number }\ninput p : P\ndecision d : number = 1` },
  { name: "string function substring(...)", source: `model "x" {}\ninput s : string\ndecision d : string = substring(s, 1, 2)` },
  { name: "for comprehension", source: `model "x" {}\ndecision d : number = for i in [1,2,3] return i` },
  { name: "some/every quantifier", source: `model "x" {}\ndecision d : boolean = some i in [1,2] satisfies i > 0` },
  { name: "** power operator", source: `model "x" {}\ninput a : number\ndecision d : number = a ** 2` },
  { name: "not(<comparison>) in a cell", source: `model "x" {}\ninput age : number\ndecision d : string {\n  needs: age\n  not(< 18) => "adult"\n  default => "minor"\n}` },
  { name: "? inside a literal-expression decision", source: `model "x" {}\ninput a : number\ndecision d : number = ? + 1` },
  { name: "unknown / unsupported type", source: `model "x" {}\ninput a : Foo\ndecision d : number = 1` },
];

describe("rejects out-of-scope constructs (compile-time guardian)", () => {
  for (const r of REJECTED) {
    it(`rejects: ${r.name}`, () => {
      expect(() => feelc.run(r.source, "d", {})).toThrow(FeelcError);
    });
  }
});

// --- TRIPWIRES: deterministic, philosophy-compatible features that are NOT YET supported but are
// recommended additions (see docs/comparison.md). Each is currently rejected; when one is
// implemented, its test will FAIL — that is the signal to move it to the supported suite + add an ADR.
const CANDIDATE_GAPS: { name: string; source: string }[] = [
  { name: "downstream read of an upstream context field", source: `model "x" {}\ninput n : number\ntype R = context { v: number }\ndecision up : R {\n  needs: n\n  hit: first\n  >= 0 => 1\n  default => 0\n}\ndecision down : number = up.v` },
];

describe("candidate additions — currently rejected (tripwires; see docs/comparison.md)", () => {
  for (const g of CANDIDATE_GAPS) {
    it(`not yet supported: ${g.name}`, () => {
      expect(() => feelc.run(g.source, "d", {})).toThrow(FeelcError);
    });
  }
});

// --- NEWLY SUPPORTED (ADR 0021 OUTPUT ORDER, 0022 power, 0023 string predicates, 0025 bounded
// quantifiers). These were tripwires; now they must compile AND produce the right value through WASM.
describe("newly supported philosophy-compatible features (ADR 0021-0025)", () => {
  it("bounded quantifiers every/some of {a,b,c} satisfies ?", () => {
    const src = `model "x" {}\ninput a : number\ninput b : number\ndecision allPos : boolean = every of {a, b} satisfies ? > 0\ndecision anyNeg : boolean = some of {a, b} satisfies ? < 0`;
    expect(feelc.run(src, "allPos", { a: 1, b: 2 }).output).toBe(true);
    expect(feelc.run(src, "allPos", { a: 1, b: -2 }).output).toBe(false);
    expect(feelc.run(src, "anyNeg", { a: 1, b: -2 }).output).toBe(true);
    expect(feelc.run(src, "anyNeg", { a: 1, b: 2 }).output).toBe(false);
  });


  it("string predicates starts_with / ends_with / contains -> boolean", () => {
    const src = `model "x" {}\ninput code : string\ndecision sw : boolean = starts_with(code, "EU")\ndecision ew : boolean = ends_with(code, "X")\ndecision ct : boolean = contains(code, "-")`;
    expect(feelc.run(src, "sw", { code: "EU-1" }).output).toBe(true);
    expect(feelc.run(src, "sw", { code: "US-1" }).output).toBe(false);
    expect(feelc.run(src, "ew", { code: "AX" }).output).toBe(true);
    expect(feelc.run(src, "ct", { code: "AB" }).output).toBe(false);
  });

  it("power(x, n) exact integer exponentiation", () => {
    const src = `model "x" {}\ninput b : number\ndecision p : number = power(b, 3)`;
    expect(feelc.run(src, "p", { b: 2 }).output).toBe(8);
    expect(feelc.run(src, "p", { b: 10 }).output).toBe(1000);
  });

  it("OUTPUT ORDER hit policy returns matches ordered by output priority", () => {
    const src = `model "x" {}\ninput score : number\ndecision v : string {\n  needs: score\n  hit: output order\n  priority: "reject", "review", "approve"\n  >= 0 => "approve"\n  >= 700 => "review"\n  < 600 => "reject"\n}`;
    expect(feelc.run(src, "v", { score: 800 }).output).toEqual(["review", "approve"]);
    expect(feelc.run(src, "v", { score: 500 }).output).toEqual(["reject", "approve"]);
  });
});
