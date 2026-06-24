import { loadEngine, type FeelcWasm } from "./loader.js";
import type {
  BatchResult,
  CheckClaim,
  CheckResult,
  CreateEngineOptions,
  EvalOptions,
  FeelcDiag,
  GraphResult,
  Inputs,
  ModelSurface,
  RequiredResult,
  RunResult,
  VerifyReport,
  VerifyResult,
} from "./types.js";

/** Thrown when the engine reports an error. Carries the structured compile diagnostic when present. */
export class FeelcError extends Error {
  readonly diag?: FeelcDiag;
  constructor(message: string, diag?: FeelcDiag) {
    super(message);
    this.name = "FeelcError";
    this.diag = diag;
  }
}

type WasmFn = (arg: string) => string;

/** Invoke a WASM function, parse its JSON, and throw FeelcError on `{error}`. */
function call<T>(fn: WasmFn, payload: unknown): T {
  const arg = typeof payload === "string" ? payload : JSON.stringify(payload);
  const data = JSON.parse(fn(arg)) as unknown;
  if (data && typeof data === "object" && "error" in data) {
    const err = (data as { error: unknown }).error;
    if (err && typeof err === "object") {
      const d = err as FeelcDiag;
      throw new FeelcError(d.message ?? "feelc error", d);
    }
    throw new FeelcError(String(err));
  }
  return data as T;
}

function toBase64(input: ArrayBuffer | Uint8Array): string {
  const bytes = input instanceof Uint8Array ? input : new Uint8Array(input);
  const buf = (globalThis as { Buffer?: { from(b: Uint8Array): { toString(enc: string): string } } }).Buffer;
  if (buf) return buf.from(bytes).toString("base64");
  let s = "";
  for (let i = 0; i < bytes.length; i++) s += String.fromCharCode(bytes[i]);
  return btoa(s);
}

function fromBase64(b64: string): Uint8Array {
  const buf = (globalThis as { Buffer?: { from(s: string, enc: string): Uint8Array } }).Buffer;
  if (buf) return new Uint8Array(buf.from(b64, "base64"));
  const bin = atob(b64);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}

/**
 * A compiled model held inside the WASM runtime by an opaque handle. Compile (or load) once, then
 * evaluate many times with no recompilation — the fast path for reactive UIs. Call `dispose()` when
 * done to free the handle.
 */
export class CompiledModel {
  #disposed = false;
  constructor(
    private readonly w: FeelcWasm,
    /** Opaque WASM-side handle. */
    readonly handle: number,
    /** Canonical model hash (hex of the IR's sha256). */
    readonly hash: string,
    /** Verification report (only set when produced via `compile`, not `load`). */
    readonly report?: VerifyReport,
    /** Number of blocking findings (only set via `compile`). */
    readonly blockers?: number,
  ) {}

  /** Evaluate a decision against inputs. */
  evaluate(decision: string, input: Inputs = {}, opts: EvalOptions = {}): RunResult {
    this.#assertLive();
    return call(this.w.evalCompiled, {
      handle: this.handle,
      decision,
      input,
      explain: opts.explain,
      full: opts.full,
    });
  }

  /**
   * Evaluate a decision against MANY input rows in ONE WASM call. Amortizes the JS↔WASM boundary +
   * JSON marshalling (the dominant per-call cost) across all rows; each row's result is identical to
   * {@link evaluate}. A row that fails yields a `{ error }` entry instead of throwing, so one bad row
   * never sinks the batch.
   */
  evaluateBatch(decision: string, inputs: Inputs[], opts: EvalOptions = {}): BatchResult {
    this.#assertLive();
    return call(this.w.evalCompiledBatch, {
      handle: this.handle,
      decision,
      inputs,
      explain: opts.explain,
      full: opts.full,
    });
  }

  /** The model surface (name + typed inputs + decisions). */
  info(): ModelSurface {
    this.#assertLive();
    return call(this.w.infoCompiled, { handle: this.handle });
  }

  /** The inputs a decision transitively needs (question-flow). */
  required(decision: string): RequiredResult {
    this.#assertLive();
    return call(this.w.requiredCompiled, { handle: this.handle, decision });
  }

  /** Serialize to a canonical `.ir.bin` (identical bytes to the native `feelc compile`). */
  export(): Uint8Array {
    this.#assertLive();
    const r = call<{ ir: string; hash: string }>(this.w.export, { handle: this.handle });
    return fromBase64(r.ir);
  }

  /** Free the WASM-side handle. Idempotent; the model is unusable afterwards. */
  dispose(): void {
    if (this.#disposed) return;
    this.#disposed = true;
    call(this.w.dispose, { handle: this.handle });
  }

  #assertLive(): void {
    if (this.#disposed) throw new FeelcError("CompiledModel has been disposed");
  }
}

/**
 * The feelc engine, running entirely in WebAssembly. Source-based methods (`run`, `verify`, ...)
 * compile on every call; `compile`/`load` return a {@link CompiledModel} for the compile-once path.
 */
export class FeelcEngine {
  constructor(private readonly w: FeelcWasm) {}

  // --- compile-at-runtime one-shots ---

  /** Compile `source` and evaluate `decision` against `input` in one call. */
  run(source: string, decision: string, input: Inputs = {}, opts: EvalOptions = {}): RunResult {
    return call(this.w.run, { rules: source, decision, input, explain: opts.explain, full: opts.full });
  }

  /** Compile + verify `source`; returns the hash, report and blocker count. */
  verify(source: string): VerifyResult {
    return call(this.w.verify, source);
  }

  /** The model surface for `source`. */
  model(source: string): ModelSurface {
    return call(this.w.model, source);
  }

  /** The decision graph for `source` (mermaid / dot / json + findings). */
  graph(source: string): GraphResult {
    return call(this.w.graph, source);
  }

  /** Source↔rule traceability (+ coverage when a spec is given). */
  trace(source: string, spec = ""): unknown {
    return call(this.w.trace, { rules: source, spec });
  }

  /** The inputs `decision` transitively needs, with metadata. */
  required(source: string, decision: string): RequiredResult {
    return call(this.w.required, { rules: source, decision });
  }

  /** Test claims against `source`. */
  check(source: string, claims: CheckClaim[]): CheckResult {
    return call(this.w.check, { rules: source, claims });
  }

  // --- compile once, evaluate many ---

  /** Compile `source` and retain it for repeated evaluation. Free it with {@link CompiledModel.dispose}. */
  compile(source: string): CompiledModel {
    const r = call<{ handle: number; hash: string; report: VerifyReport; blockers: number }>(
      this.w.compile,
      source,
    );
    return new CompiledModel(this.w, r.handle, r.hash, r.report, r.blockers);
  }

  /** Load a precompiled `.ir.bin` (the bytes from {@link CompiledModel.export} or `feelc compile`). */
  load(bytes: ArrayBuffer | Uint8Array): CompiledModel {
    const r = call<{ handle: number; hash: string }>(this.w.load, { ir: toBase64(bytes) });
    return new CompiledModel(this.w, r.handle, r.hash);
  }
}

/** Boot the WASM engine. Returns a ready-to-use {@link FeelcEngine}. */
export async function createEngine(opts: CreateEngineOptions = {}): Promise<FeelcEngine> {
  const w = await loadEngine(opts);
  return new FeelcEngine(w);
}
