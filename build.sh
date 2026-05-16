#!/bin/bash
set -e

# Configuration
BINARY_NAME="life"
VERSION=${1:-"dev"}
BUILD_DIR="./build"
BUILD_OS=$(uname -s | tr '[:upper:]' '[:lower:]')

# Target architectures: (OS ARCH)
TARGETS=(
    "linux amd64"
    "linux arm64"
    "darwin amd64"
    "darwin arm64"
    "windows amd64"
)

echo "Building $BINARY_NAME version: $VERSION"
echo "Creating build directory: $BUILD_DIR"
rm -rf "$BUILD_DIR"
mkdir -p "$BUILD_DIR"

for target in "${TARGETS[@]}"; do
    OS=$(echo $target | cut -d' ' -f1)
    ARCH=$(echo $target | cut -d' ' -f2)

    OUTPUT="$BUILD_DIR/${BINARY_NAME}-${VERSION}-${OS}-${ARCH}"
    if [ "$OS" = "windows" ]; then
        OUTPUT="${OUTPUT}.exe"
    fi

    # Only enable CGO when building for the same OS
    CGO_ENABLED=0
    if [ "$OS" = "$BUILD_OS" ]; then
        CGO_ENABLED=1
    fi

    echo "Building for $OS/$ARCH -> $OUTPUT (CGO=$CGO_ENABLED)"

    GOOS=$OS GOARCH=$ARCH CGO_ENABLED=$CGO_ENABLED go build \
        -ldflags="-s -w -X main.Version=$VERSION" \
        -o "$OUTPUT" \
        ./cmd/life
done

echo ""
echo "Build complete! Binaries in $BUILD_DIR:"
ls -lh "$BUILD_DIR/"
echo ""
echo "SHA256 checksums:"
cd "$BUILD_DIR"
shasum -a 256 * > checksums.txt
cat checksums.txt
