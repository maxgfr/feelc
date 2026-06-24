#!/usr/bin/env bash
# Publish the feelc npm package for a release. Called by .releaserc.json's exec publishCmd:
#   scripts/release-publish.sh <version>
#
# Auth precedence:
#   1. $NPM_TOKEN set  -> publish with that token (an npm "Automation" token bypasses 2FA). Fatal on
#      failure (a configured publish must succeed).
#   2. $NPM_TOKEN empty -> npm OIDC "trusted publishing" (needs a trusted publisher configured for the
#      `feelc` package against this workflow + `id-token: write`). If it is not configured/matching, the
#      publish is SKIPPED with a warning so the binary release still succeeds.
set -euo pipefail
VERSION="${1:?usage: release-publish.sh <version>}"

# Set the package version to the release version (transient; not committed).
npm version "$VERSION" --no-git-tag-version -w feelc >/dev/null

if [ -n "${NPM_TOKEN:-}" ]; then
  echo "npm: publishing feelc@$VERSION with an automation token"
  npm config set //registry.npmjs.org/:_authToken "$NPM_TOKEN"
  npm publish -w feelc --access public --provenance
else
  echo "npm: NPM_TOKEN not set — attempting OIDC trusted publishing for feelc@$VERSION"
  if ! npm publish -w feelc --access public --provenance; then
    echo "::warning title=npm publish skipped::feelc@$VERSION was NOT published to npm. Add an NPM_TOKEN automation secret (npmjs -> Access Tokens -> Generate -> Automation), or configure OIDC trusted publishing (npmjs -> package feelc -> Trusted Publishing -> repo maxgfr/feelc, workflow npm.yml). The cross-platform binaries were released."
  fi
fi
