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

# Idempotent: if this exact version is already on the registry (e.g. a re-run after a partial release),
# there is nothing to do — npm would reject a republish anyway.
if npm view "feelc@${VERSION}" version >/dev/null 2>&1; then
  echo "npm: feelc@${VERSION} is already published — nothing to do."
  exit 0
fi

# Set the package version to the release version (transient; not committed).
npm version "$VERSION" --no-git-tag-version -w feelc >/dev/null

if [ -n "${NPM_TOKEN:-}" ]; then
  echo "npm: publishing feelc@$VERSION with an automation token"
  npm config set //registry.npmjs.org/:_authToken "$NPM_TOKEN"
  npm publish -w feelc --access public --provenance # fatal on failure — a configured publish must succeed
else
  echo "npm: NPM_TOKEN not set — attempting OIDC trusted publishing for feelc@$VERSION"
  if ! npm publish -w feelc --access public --provenance; then
    echo "::error title=npm publish failed::feelc@$VERSION was NOT published (no NPM_TOKEN and OIDC trusted publishing is not configured/authorized — npm returned ENEEDAUTH). Fix: add an NPM_TOKEN automation secret (npmjs -> Access Tokens -> Generate -> Automation), OR configure OIDC trusted publishing (npmjs -> package feelc -> Trusted Publishing -> repo maxgfr/feelc, workflow npm.yml), then re-run the 'publish-npm' workflow. The cross-platform binaries were released."
    exit 0 # keep the binary release green; the ::error:: annotation surfaces the miss instead of hiding it
  fi
fi

# Confirm the publish actually landed on the registry (catches a silent no-op).
if npm view "feelc@${VERSION}" version >/dev/null 2>&1; then
  echo "npm: ✅ feelc@${VERSION} is live on the registry."
else
  echo "::warning title=npm verify::feelc@${VERSION} not yet visible on the registry (propagation delay?). Verify with: npm view feelc version"
fi
