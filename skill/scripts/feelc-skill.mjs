#!/usr/bin/env node
// Wrapper portable ZÉRO-DÉPENDANCE de la skill feelc-rules.
// Rôle : localiser le binaire `feelc` puis lui relayer les arguments (run/verify/serve/version),
// en héritant stdin/stdout/stderr et le code de sortie. Aucune logique métier ici : feelc est
// l'oracle déterministe. Harness-agnostique (Claude Code, Codex, Cursor, CLI…).
//
// Découverte de feelc, dans l'ordre :
//   1. $FEELC_BIN (chemin explicite)
//   2. `feelc` sur le PATH
//   3. binaire déjà buildé à la racine du dépôt : ../../feelc
//   4. build depuis les sources du dépôt : `go build -o ../../feelc ./cmd/feelc`
// (3 et 4 fonctionnent quand la skill tourne DANS le dépôt feelc ; en installation autonome,
//  fournis $FEELC_BIN ou mets feelc sur le PATH — voir references/install.md.)
// Sinon : message d'installation + exit 1.

import { spawnSync } from "node:child_process";
import { existsSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const here = dirname(fileURLToPath(import.meta.url));
const repoRoot = join(here, "..", ".."); // feelc/skill/scripts -> racine du dépôt feelc/
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
    "feelc-rules : binaire `feelc` introuvable.\n" +
      "Fournis-le par l'un de ces moyens :\n" +
      "  - export FEELC_BIN=/chemin/vers/feelc\n" +
      "  - installe feelc sur le PATH\n" +
      "  - place le dépôt feelc à côté de feelc-rules/ (build auto via `go build`)\n" +
      "Voir references/install.md.\n"
  );
  process.exit(1);
}

const res = spawnSync(bin, process.argv.slice(2), { stdio: "inherit" });
process.exit(res.status === null ? 1 : res.status);
