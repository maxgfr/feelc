# Releasing

feelc has **one release pipeline** — the `release` job in `.github/workflows/npm.yml`. On every push to
`main`, [semantic-release](https://semantic-release.gitbook.io/) reads the
[Conventional Commits](https://www.conventionalcommits.org/) since the last tag and computes the next
version. It then writes & **commits `CHANGELOG.md` back to `main`** (`[skip ci]`), tags the release,
[goreleaser](https://goreleaser.com/) builds the cross-platform binaries (linux/darwin/windows ×
amd64/arm64, CGO-free), a **GitHub Release** is cut (with the generated release notes and the binaries
attached as assets), and **`feelc`** is published to npm via **OIDC trusted publishing** (no token, with
provenance). The npm package version is unified with the repo version (the git tag).

```
push to main ──▶ ci (build + test) ──▶ release: semantic-release
   @semantic-release/changelog ─▶ write CHANGELOG.md
   @semantic-release/git       ─▶ commit + push CHANGELOG.md to main ([skip ci])
   (core)                      ─▶ tag vX.Y.Z + push
   @semantic-release/exec      ─▶ goreleaser (binaries -> dist/) + npm version X.Y.Z + npm publish (OIDC)
   @semantic-release/github    ─▶ GitHub Release (notes) + upload dist/* (tar.gz, zip, checksums.txt)
```

The plugin chain lives in [`.releaserc.json`](.releaserc.json); the plugins themselves are pinned in the
root `package.json` `devDependencies` and installed by `npm ci` in CI.

- `feat:` → minor, `fix:` → patch, `feat!:`/`BREAKING CHANGE:` → major. `docs:`/`ci:`/`test:`/`chore:`
  do **not** trigger a release.
- The `CHANGELOG.md` commit-back uses `[skip ci]`, so it never re-triggers the workflow. It needs
  `contents: write` (already granted) and a `main` that accepts a direct push from `GITHUB_TOKEN` (no
  branch-protection rule blocking the Actions bot).
- goreleaser no longer creates the GitHub Release (`release.disable` in `.goreleaser.yaml`); it only
  builds `dist/`. `@semantic-release/github` owns the Release so the notes match the changelog.
- **Versioning is unified**: the binary and the npm package share the version derived from git tags
  (semantic-release reads the last `vX.Y.Z` tag, not `package.json`). The committed
  `packages/engine/package.json` version is a placeholder; the release bumps it to `X.Y.Z` in CI right
  before `npm publish`, so each release publishes the correct version without a commit-back loop.

## npm auth — pick one (one-time)

The publish (`scripts/release-publish.sh`) supports two auth methods, in this order:

**1. NPM_TOKEN automation token (recommended — works with 2FA).**
On npmjs.com → Access Tokens → Generate New Token → **Automation** (automation tokens bypass 2FA). Add
it as a **repository secret** named `NPM_TOKEN` (GitHub → Settings → Secrets and variables → Actions).
That's it — releases publish reliably.

**2. OIDC trusted publishing (no token).**
On npmjs.com → the `feelc` package → Settings → Trusted Publishing → add a GitHub Actions publisher:
repo `maxgfr/feelc`, workflow **`npm.yml`**, environment *(empty)*. If the config matches exactly, npm
publishes via OIDC with no secret. If it isn't configured/matching, the publish is **skipped with a
warning** (the binaries still release) — so a missing/incorrect OIDC config never turns the pipeline red.

> Note: a `v*` tag pushed by semantic-release uses `GITHUB_TOKEN`, and tags pushed by `GITHUB_TOKEN`
> don't trigger other workflows — that's why the npm publish lives in the **same** `release` job that
> cuts the release, not in a separate tag-triggered workflow.

## Cutting a release

Merge to `main` with a `feat:`/`fix:` commit. Preview what semantic-release would do — computes the
version and prints the release notes, writes/pushes nothing (needs `GITHUB_TOKEN` in the environment):

```bash
GITHUB_TOKEN=$(gh auth token) npm run release:dry   # = semantic-release --dry-run --no-ci
```

## Manual / re-publish (npm only)

To publish the npm package by hand, set the version first (it must be higher than the latest on npm):

```bash
npm version <X.Y.Z> --no-git-tag-version -w feelc
npm publish -w feelc --access public      # prepublishOnly rebuilds the WASM + TS
```
