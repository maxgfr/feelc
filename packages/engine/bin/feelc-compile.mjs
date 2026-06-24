#!/usr/bin/env node
// feelc-compile — compile a .rules source to a precompiled .ir.bin artifact, using the WASM engine
// (no Go toolchain required). The output is byte-identical to the native `feelc compile`.
//
//   feelc-compile rules.rules                 # -> rules.ir.bin
//   feelc-compile rules.rules -o model.ir.bin
//   feelc-compile rules.rules --check         # fail (exit 1) if the model has verification blockers
import { readFile, writeFile } from "node:fs/promises";
import { createEngine } from "../dist/index.js";

function parseArgs(argv) {
  const args = { check: false };
  for (let i = 0; i < argv.length; i++) {
    const a = argv[i];
    if (a === "-o" || a === "--out") args.out = argv[++i];
    else if (a === "--check") args.check = true;
    else if (a === "-h" || a === "--help") args.help = true;
    else if (!a.startsWith("-")) args.input = a;
    else throw new Error(`unknown flag: ${a}`);
  }
  return args;
}

const USAGE = "usage: feelc-compile <file.rules> [-o <out.ir.bin>] [--check]";

async function main() {
  const args = parseArgs(process.argv.slice(2));
  if (args.help || !args.input) {
    console.log(USAGE);
    process.exit(args.input ? 0 : 1);
  }
  const out = args.out ?? args.input.replace(/\.rules$/, "") + ".ir.bin";
  const source = await readFile(args.input, "utf8");

  const engine = await createEngine();
  const model = engine.compile(source);
  try {
    if (model.blockers && model.blockers > 0) {
      const msg = `${model.blockers} verification blocker(s)`;
      if (args.check) {
        console.error(`feelc-compile: ${msg} — not writing ${out}`);
        process.exit(1);
      }
      console.warn(`feelc-compile: warning: ${msg}`);
    }
    await writeFile(out, model.export());
    console.log(`feelc-compile: wrote ${out} (hash ${model.hash.slice(0, 12)}…)`);
  } finally {
    model.dispose();
  }
}

main().catch((err) => {
  console.error(`feelc-compile: ${err?.message ?? err}`);
  process.exit(1);
});
