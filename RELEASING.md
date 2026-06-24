# Releasing

feelc has **one release pipeline** — the `release` job in `.github/workflows/npm.yml`. On every push to
`main`, [semantic-release](https://semantic-release.gitbook.io/) reads the
[Conventional Commits](https://www.conventionalcommits.org/) since the last tag, computes the next
version, tags it, [goreleaser](https://goreleaser.com/) publishes the cross-platform binaries
(linux/darwin/windows × amd64/arm64, CGO-free) to the GitHub Release, and **`feelc`** is published to
npm via **OIDC trusted publishing** (no token, with provenance). The npm package version is unified with
the repo version (the git tag).

```
push to main ──▶ ci (build + test) ──▶ release:
                                          semantic-release (version from commits)
                                            └─▶ tag vX.Y.Z + push
                                            └─▶ goreleaser  (GitHub Release + binaries)
                                            └─▶ npm version X.Y.Z + npm publish feelc (OIDC)
```

- `feat:` → minor, `fix:` → patch, `feat!:`/`BREAKING CHANGE:` → major. `docs:`/`ci:`/`test:`/`chore:`
  do **not** trigger a release.
- **Versioning is unified**: the binary and the npm package share the version derived from git tags
  (semantic-release reads the last `vX.Y.Z` tag, not `package.json`). The committed
  `packages/engine/package.json` version is a placeholder; the release bumps it to `X.Y.Z` in CI right
  before `npm publish`, so each release publishes the correct version without a commit-back loop.

## OIDC trusted publishing (one-time)

`npm.yml` publishes with **no `NPM_TOKEN`** — it uses GitHub OIDC. This is configured **against the
`npm.yml` workflow** on npmjs.com (the package → Settings → Trusted Publishing → add a GitHub Actions
publisher: repo `maxgfr/feelc`, workflow `npm.yml`). Because the publish step lives in `npm.yml`, the
OIDC identity matches and npm accepts the publish.

> Note: a `v*` tag pushed by semantic-release uses `GITHUB_TOKEN`, and tags pushed by `GITHUB_TOKEN`
> don't trigger other workflows — that's why the npm publish lives in the **same** `release` job that
> cuts the release, not in a separate tag-triggered workflow.

## Cutting a release

Merge to `main` with a `feat:`/`fix:` commit. Preview what semantic-release would do (no publish):

```bash
npx semantic-release --dry-run
```

## Manual / re-publish (npm only)

To publish the npm package by hand, set the version first (it must be higher than the latest on npm):

```bash
npm version <X.Y.Z> --no-git-tag-version -w feelc
npm publish -w feelc --access public      # prepublishOnly rebuilds the WASM + TS
```
