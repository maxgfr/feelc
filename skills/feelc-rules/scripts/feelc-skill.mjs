#!/usr/bin/env node
// Portable ZERO-DEPENDENCY wrapper for the feelc-rules skill.
// Role: locate the `feelc` binary then relay arguments to it (run/verify/serve/version),
// inheriting stdin/stdout/stderr and the exit code. No business logic here: feelc is
// the deterministic oracle. Harness-agnostic (Claude Code, Codex, Cursor, CLI…).
//
// feelc discovery, in order:
//   1. $FEELC_BIN (explicit path)
//   2. `feelc` on the PATH
//   3. binary already built at the repo root: ../../../feelc
//   4. build from the repo sources: `go build -o ../../../feelc ./cmd/feelc`
// (3 and 4 work when the skill runs INSIDE the feelc repo; in a standalone install,
//  provide $FEELC_BIN or put feelc on the PATH — see references/install.md.)
// Otherwise: installation message + exit 1.

import { spawnSync } from "node:child_process";
import { existsSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const here = dirname(fileURLToPath(import.meta.url));
const repoRoot = join(here, "..", "..", ".."); // feelc/skills/feelc-rules/scripts -> feelc/ repo root
const localBin = join(repoRoot, "feelc");

function onPath(cmd) {
  const r = spawnSync(process.platform === "win32" ? "where" : "command", process.platform === "win32" ? [cmd] : ["-v", cmd], { encoding: "utf8" });
  return r.status === 0 ? (r.stdout || "").trim().split("\n")[0] : null;
}

function tryBuildLocal() {
  if (!existsSync(join(repoRoot, "go.mod"))) return null;
  if (!onPath("go")) return null;
  const b = spawnSync("go", ["build", "-o", localBin, "./cmd/feelc"], { cwd: repoRoot, stdio: "inherit" });
  return b.status === 0 && existsSync(localBin) ? localBin : null;
}

function locate() {
  if (process.env.FEELC_BIN && existsSync(process.env.FEELC_BIN)) return process.env.FEELC_BIN;
  const onp = onPath("feelc");
  if (onp) return onp;
  if (existsSync(localBin)) return localBin;
  return tryBuildLocal();
}

const bin = locate();
if (!bin) {
  process.stderr.write(
    "feelc-rules: `feelc` binary not found.\n" +
      "Provide it via one of these means:\n" +
      "  - export FEELC_BIN=/path/to/feelc\n" +
      "  - install feelc on the PATH\n" +
      "  - place the feelc repo next to feelc-rules/ (auto build via `go build`)\n" +
      "See references/install.md.\n"
  );
  process.exit(1);
}

const res = spawnSync(bin, process.argv.slice(2), { stdio: "inherit" });
process.exit(res.status === null ? 1 : res.status);
