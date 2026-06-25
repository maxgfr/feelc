#!/usr/bin/env bash
# Publish the feelc npm package for a release. Called by .releaserc.json's @semantic-release/exec
# publishCmd (after the tag + goreleaser binary build, before @semantic-release/github cuts the Release),
# and by the manual `publish-npm` recovery job in .github/workflows/npm.yml:
#   scripts/release-publish.sh <version>
#
# Auth precedence:
#   1. $NPM_TOKEN set  -> publish with that token (an npm "Automation" token bypasses 2FA). Fatal on
#      failure (a configured publish must succeed).
#   2. $NPM_TOKEN empty -> npm OIDC "trusted publishing" (needs a trusted publisher configured for the
#      `feelc` package against this workflow + `id-token: write`). If it is not configured/matching, npm
#      returns ENEEDAUTH; we surface a loud ::error:: annotation but keep exit 0 so the binary release
#      still goes out — re-run the `publish-npm` workflow once auth is in place to catch npm up.
set -euo pipefail
VERSION="${1:?usage: release-publish.sh <version>}"

# Resolve an OIDC-capable npm for the publish. OIDC "trusted publishing" needs npm >= 11.5.1. But
# semantic-release/exec runs this script with the repo's ./node_modules/.bin at the FRONT of PATH, and
# @semantic-release/npm (a transitive dep of semantic-release) plants an npm 10.x there. A bare `npm`
# therefore resolves to that old npm, which has no OIDC support: it silently skips the token exchange
# and dies with ENEEDAUTH. The release workflow upgrades the GLOBAL npm (`npm install -g npm@latest`),
# which lives next to the `node` binary and is never shadowed by node_modules — prefer it.
NPM=npm
_global_npm="$(dirname "$(command -v node)")/npm"
if [ -x "$_global_npm" ]; then NPM="$_global_npm"; fi
echo "npm: using $("$NPM" --version) ($NPM)"

# Idempotent: if this exact version is already on the registry (e.g. a re-run after a partial release),
# there is nothing to do — npm would reject a republish anyway.
if "$NPM" view "feelc@${VERSION}" version >/dev/null 2>&1; then
  echo "npm: feelc@${VERSION} is already published — nothing to do."
  exit 0
fi

# Set the package version to the release version (transient; not committed).
"$NPM" version "$VERSION" --no-git-tag-version -w feelc >/dev/null

if [ -n "${NPM_TOKEN:-}" ]; then
  echo "npm: publishing feelc@$VERSION with an automation token"
  "$NPM" config set //registry.npmjs.org/:_authToken "$NPM_TOKEN"
  "$NPM" publish -w feelc --access public --provenance # fatal on failure — a configured publish must succeed
else
  echo "npm: NPM_TOKEN not set — attempting OIDC trusted publishing for feelc@$VERSION"
  # OIDC also needs the GitHub Actions OIDC request env vars (granted by `permissions: id-token: write`).
  # If they are absent, npm silently skips the exchange and falls through to ENEEDAUTH — so assert them
  # explicitly; their absence is a workflow/permissions issue, not an npmjs one.
  if [ -n "${ACTIONS_ID_TOKEN_REQUEST_URL:-}" ] && [ -n "${ACTIONS_ID_TOKEN_REQUEST_TOKEN:-}" ]; then
    echo "npm: OIDC request env present — 'id-token: write' is in effect; npm will attempt the OIDC token exchange."
  else
    echo "::error title=OIDC env missing::ACTIONS_ID_TOKEN_REQUEST_URL/TOKEN are not set in this job, so npm cannot do OIDC trusted publishing (it will fail with ENEEDAUTH). The job needs 'permissions: id-token: write' and must not be a context where GitHub withholds the OIDC token (e.g. a fork PR)."
  fi
  if ! "$NPM" publish -w feelc --access public --provenance; then
    echo "::error title=npm publish failed::feelc@$VERSION was NOT published — npm returned ENEEDAUTH. Either the npm running the publish is too old for OIDC (need >= 11.5.1 — see the 'npm: using ...' line above), the OIDC request env was missing (annotation above), or npmjs rejected the exchange because no trusted publisher matches repo maxgfr/feelc + workflow npm.yml on refs/heads/main (fix at npmjs -> package feelc -> Settings -> Trusted Publishing). Fallback: add an NPM_TOKEN automation secret, then re-run the 'publish-npm' workflow. The cross-platform binaries were released."
    exit 0 # keep the binary release green; the ::error:: annotation surfaces the miss instead of hiding it
  fi
fi

# Confirm the publish actually landed on the registry (catches a silent no-op).
if "$NPM" view "feelc@${VERSION}" version >/dev/null 2>&1; then
  echo "npm: ✅ feelc@${VERSION} is live on the registry."
else
  echo "::warning title=npm verify::feelc@${VERSION} not yet visible on the registry (propagation delay?). Verify with: npm view feelc version"
fi
