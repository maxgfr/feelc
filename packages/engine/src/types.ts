// Public types mirroring the JSON shapes the WASM engine returns (single source of truth: the Go
// packages internal/modelinfo, internal/verify, internal/check, internal/diag).

/** A JSON value accepted as a decision input. */
export type InputValue = number | string | boolean | null | InputObject | InputValue[];
export interface InputObject {
  [key: string]: InputValue;
}
/** Inputs to a decision, keyed by input name. */
export type Inputs = Record<string, InputValue>;

export interface EvalOptions {
  /** Include a justification trace (winning rule + cells). */
  explain?: boolean;
  /** Include the full evaluation trace (every decision). Takes precedence over `explain`. */
  full?: boolean;
}

export interface RunResult {
  decision: string;
  /**
   * The decision output. Exact decimals arrive as JS numbers (float64) — very large/precise values
   * can lose precision crossing into JS. Strings/booleans/objects pass through unchanged.
   */
  output: unknown;
  /** Present when `explain`/`full` was requested and tracing succeeded. */
  trace?: unknown;
  /** Present when tracing was requested but failed (the `output` is still valid). */
  traceError?: string;
}

/** A model input as surfaced for widgets, question-flow and docs (internal/modelinfo.InputInfo). */
export interface InputInfo {
  name: string;
  type: "number" | "string" | "boolean" | "context" | "date" | "duration" | (string & {});
  domain?: string;
  unit?: string;
  title?: string;
  question?: string;
  doc?: string;
  source?: string;
}

/** A decision's kind/hit-policy and metadata (internal/modelinfo.DecInfo). */
export interface DecisionInfo {
  name: string;
  kind: "table" | "literal-expr" | (string & {});
  hitPolicy?: string;
  deps?: string[];
  unit?: string;
  title?: string;
  doc?: string;
  source?: string;
}

/** The model surface: name + typed inputs + decisions. */
export interface ModelSurface {
  name: string;
  inputs: InputInfo[];
  decisions: DecisionInfo[];
}

/** The inputs a decision transitively needs (question-flow / simulator). */
export interface RequiredResult {
  decision: string;
  inputs: InputInfo[];
}

/** A verification finding (advisory unless it is a blocker). */
export interface VerifyFinding {
  severity?: string;
  code?: string;
  message?: string;
  [key: string]: unknown;
}

/** A verification report (internal/verify.Report). Shape kept loose beyond findings. */
export interface VerifyReport {
  findings?: VerifyFinding[];
  [key: string]: unknown;
}

export interface VerifyResult {
  hash: string;
  report: VerifyReport;
  /** Number of blocking findings. > 0 means the model is not safe to publish. */
  blockers: number;
}

export interface GraphResult {
  mermaid: string;
  dot: string;
  graph: unknown;
  findings?: VerifyFinding[];
  blockers: number;
}

/** A claim to test against a model (internal/check.Claim). */
export interface CheckClaim {
  /** Original natural-language phrasing, for traceability. */
  desc?: string;
  decision: string;
  input: Inputs;
  expect: unknown;
}

export interface CheckResult {
  report: unknown;
  blockers: number;
}

/** Structured compile diagnostic (internal/diag.Error), attached to FeelcError when available. */
export interface FeelcDiag {
  message?: string;
  code?: string;
  file?: string;
  line?: number;
  col?: number;
  [key: string]: unknown;
}

export interface CreateEngineOptions {
  /** Override the .wasm location (URL or path). Useful for custom hosting. */
  wasmUrl?: string | URL;
  /**
   * Provide the .wasm directly: bytes, a compiled module, or a fetch Response. Required in edge
   * runtimes (Cloudflare Workers, Deno) where the default file/URL resolution doesn't apply — import
   * the .wasm as a module and pass it here.
   */
  wasmBinary?: ArrayBuffer | Uint8Array | WebAssembly.Module | Response | Promise<Response>;
  /**
   * Global name the engine registers under (default "feelc"). Set a unique token to run multiple
   * isolated instances in the same realm. (Web Workers — one engine per worker — are the recommended
   * way to scale.)
   */
  instanceToken?: string;
}
