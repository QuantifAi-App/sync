#!/usr/bin/env bash
# build-pkg.sh — Build a macOS .pkg installer for quantifai-sync.
#
# Usage: ./build-pkg.sh <version> <arch>
#   e.g.: ./build-pkg.sh 1.2.0 arm64
#         ./build-pkg.sh 1.2.0 x86_64
#
# Produces: quantifai-sync-<version>-<arch>.pkg
#
# Requirements: Xcode Command Line Tools (pkgbuild, productbuild)
set -euo pipefail

VERSION="${1:?Usage: $0 <version> <arch>}"
ARCH="${2:?Usage: $0 <version> <arch>}"

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
SHIPPER_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
BUILD_DIR="${SHIPPER_DIR}/pkg-build"
STAGING="${BUILD_DIR}/staging"
COMPONENT_PKG="${BUILD_DIR}/component.pkg"
OUTPUT_PKG="${SHIPPER_DIR}/quantifai-sync-${VERSION}-${ARCH}.pkg"

# Map uname arch to Go arch
case "${ARCH}" in
    arm64|aarch64) GOARCH="arm64" ;;
    x86_64|amd64)  GOARCH="amd64" ;;
    *)             echo "error: unsupported arch: ${ARCH}"; exit 1 ;;
esac

echo "Building quantifai-sync ${VERSION} for darwin/${GOARCH}..."

# Clean previous build
rm -rf "${BUILD_DIR}"
mkdir -p "${STAGING}/usr/local/bin"

# Build binary
cd "${SHIPPER_DIR}"
GOOS=darwin GOARCH="${GOARCH}" go build \
    -ldflags "-X github.com/quantifai/sync/cmd.Version=${VERSION}" \
    -o "${STAGING}/usr/local/bin/quantifai-sync" .

echo "Binary built: $(file "${STAGING}/usr/local/bin/quantifai-sync")"

# Create component package
pkgbuild \
    --root "${STAGING}" \
    --install-location "/" \
    --scripts "${SCRIPT_DIR}/scripts" \
    --identifier "com.quantifai.sync" \
    --version "${VERSION}" \
    "${COMPONENT_PKG}"

# Create product archive with distribution.xml
productbuild \
    --distribution "${SCRIPT_DIR}/distribution.xml" \
    --resources "${SCRIPT_DIR}/resources" \
    --package-path "${BUILD_DIR}" \
    "${OUTPUT_PKG}"

# Clean up intermediate files
rm -rf "${BUILD_DIR}"

echo ""
echo "Package built: ${OUTPUT_PKG}"
echo "Size: $(du -h "${OUTPUT_PKG}" | awk '{print $1}')"
echo ""
echo "Install with: sudo installer -pkg ${OUTPUT_PKG} -target /"
echo "Or double-click the .pkg file in Finder."
