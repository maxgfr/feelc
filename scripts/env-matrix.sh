#!/usr/bin/env bash
# Validate the feelc engine across runtimes (see docs/environments.md). Run from the repo root:
#   bash scripts/env-matrix.sh            # Go + Node + Bun + Vite + Deno
#   WITH_DOCKER=1 bash scripts/env-matrix.sh   # + build & health-check the Docker image
set -euo pipefail
cd "$(dirname "$0")/.."

echo "== Go (native, deterministic) =="
make test

echo "== WASM build =="
make wasm
npm run build -w feelc

echo "== Node: engine unit tests + WASM-vs-CLI parity =="
npm run test -w feelc
npm run test -w @feelc-examples/node-smoke

echo "== Bun: WASM-vs-CLI parity =="
if command -v bun >/dev/null 2>&1; then (cd examples/node-smoke && bun run test); else echo "skip: bun not installed"; fi

echo "== Vite: browser bundle =="
npm run build -w @feelc-examples/browser-vite

echo "== Deno: edge-style wasmBinary =="
if command -v deno >/dev/null 2>&1; then deno run --allow-read packages/engine/scripts/deno-smoke.mjs; else echo "skip: deno not installed"; fi

if [ "${WITH_DOCKER:-0}" = "1" ]; then
  echo "== Docker: image + health =="
  docker build -t feelc:matrix .
  CID=$(docker run -d --rm -p 8099:8080 -v "$PWD/sample-project:/work" feelc:matrix)
  trap 'docker stop "$CID" >/dev/null 2>&1 || true' EXIT
  for _ in $(seq 1 30); do
    [ "$(curl -s -o /dev/null -w '%{http_code}' localhost:8099/readyz 2>/dev/null)" = "200" ] && break
    sleep 0.5
  done
  curl -fsS localhost:8099/readyz >/dev/null && echo "docker /readyz OK"
fi

echo "ALL ENVIRONMENTS OK"
