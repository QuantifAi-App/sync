#!/usr/bin/env bash
# update-formula.sh — Update the Homebrew formula with release SHA256 hashes.
#
# Usage: ./update-formula.sh <version>
#   e.g.: ./update-formula.sh 1.2.0
#
# Downloads release assets from GitHub, computes SHA256 checksums, and
# updates the formula template in-place. Intended for CI/release automation.
set -euo pipefail

VERSION="${1:?Usage: $0 <version>}"
REPO="quantifai-app/sync"
FORMULA_DIR="$(cd "$(dirname "$0")" && pwd)"
FORMULA="${FORMULA_DIR}/quantifai-sync.rb"
BASE_URL="https://github.com/${REPO}/releases/download/v${VERSION}"

PLATFORMS=(
    "darwin-arm64"
    "darwin-amd64"
    "linux-arm64"
    "linux-amd64"
)

TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

echo "Updating formula for v${VERSION}..."

# Start with a copy of the template
cp "${FORMULA}" "${FORMULA}.tmp"

# Replace version
sed -i.bak "s/VERSION/${VERSION}/g" "${FORMULA}.tmp"

for platform in "${PLATFORMS[@]}"; do
    asset="quantifai-sync-${platform}.tar.gz"
    url="${BASE_URL}/${asset}"
    dest="${TMPDIR}/${asset}"

    echo "  Downloading ${asset}..."
    if curl -sSL -f -o "${dest}" "${url}"; then
        sha=$(shasum -a 256 "${dest}" | awk '{print $1}')
        echo "  SHA256: ${sha}"

        # Convert platform to placeholder name (e.g., darwin-arm64 -> SHA256_DARWIN_ARM64)
        placeholder="SHA256_$(echo "${platform}" | tr '[:lower:]-' '[:upper:]_')"
        sed -i.bak "s/${placeholder}/${sha}/g" "${FORMULA}.tmp"
    else
        echo "  WARNING: Could not download ${asset} — leaving placeholder"
    fi
done

# Finalize
rm -f "${FORMULA}.tmp.bak"
mv "${FORMULA}.tmp" "${FORMULA}"

echo "Formula updated: ${FORMULA}"
echo "Review the changes, then commit and push to your Homebrew tap."
