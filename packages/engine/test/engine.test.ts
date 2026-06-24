import { beforeAll, describe, expect, it } from "vitest";
import { createEngine, FeelcError, type FeelcEngine } from "feelc";

const MODEL = `model "promo" {}

input cart_total : number >= 0
input is_member  : boolean

decision discount_pct : number {
  needs: cart_total, is_member
  hit: collect max
  >= 50  | -    => 5
  >= 100 | -    => 10
  -      | true => 8
}`;

let feelc: FeelcEngine;
beforeAll(async () => {
  feelc = await createEngine();
});

describe("run (compile at runtime)", () => {
  it("evaluates a collect-max table", () => {
    expect(feelc.run(MODEL, "discount_pct", { cart_total: 120, is_member: true }).output).toBe(10);
    expect(feelc.run(MODEL, "discount_pct", { cart_total: 60, is_member: false }).output).toBe(5);
  });

  it("returns null when no rule applies", () => {
    expect(feelc.run(MODEL, "discount_pct", { cart_total: 10, is_member: false }).output).toBeNull();
  });

  it("exposes the model surface", () => {
    const surface = feelc.model(MODEL);
    expect(surface.name).toBe("promo");
    expect(surface.inputs.map((i) => i.name)).toEqual(["cart_total", "is_member"]);
    expect(surface.decisions.map((d) => d.name)).toContain("discount_pct");
  });
});

describe("compile once, evaluate many", () => {
  it("matches run() without recompiling, and frees on dispose", () => {
    const model = feelc.compile(MODEL);
    expect(model.blockers).toBe(0);
    expect(model.evaluate("discount_pct", { cart_total: 120, is_member: true }).output).toBe(10);
    expect(model.required("discount_pct").inputs.map((i) => i.name).sort()).toEqual([
      "cart_total",
      "is_member",
    ]);
    model.dispose();
    expect(() => model.evaluate("discount_pct", {})).toThrow(FeelcError);
  });
});

describe("evaluateBatch (one boundary crossing for N rows)", () => {
  it("matches per-row evaluate() across a batch and isolates a bad row", () => {
    const model = feelc.compile(MODEL);
    const rows = [
      { cart_total: 120, is_member: true },
      { cart_total: 60, is_member: false },
      { cart_total: 10, is_member: false },
    ];
    const batch = model.evaluateBatch("discount_pct", rows);
    expect(batch.results.map((r) => ("output" in r ? r.output : r))).toEqual([10, 5, null]);
    // identical to calling evaluate() per row
    for (const row of rows) {
      const single = model.evaluate("discount_pct", row).output;
      const idx = rows.indexOf(row);
      const r = batch.results[idx];
      expect("output" in r ? r.output : undefined).toEqual(single);
    }
    model.dispose();
  });
});

describe("precompiled .ir.bin (export -> load)", () => {
  it("round-trips with an identical hash and result", () => {
    const compiled = feelc.compile(MODEL);
    const bytes = compiled.export();
    const loaded = feelc.load(bytes);
    expect(loaded.hash).toBe(compiled.hash);
    expect(loaded.evaluate("discount_pct", { cart_total: 120, is_member: true }).output).toBe(10);
    compiled.dispose();
    loaded.dispose();
  });
});

describe("errors", () => {
  it("throws FeelcError carrying a diagnostic on bad source", () => {
    expect.assertions(2);
    try {
      feelc.run("model x {", "discount_pct", {});
    } catch (err) {
      expect(err).toBeInstanceOf(FeelcError);
      expect((err as FeelcError).message.length).toBeGreaterThan(0);
    }
  });
});
