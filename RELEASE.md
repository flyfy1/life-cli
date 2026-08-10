# Release Process

This document describes how to build and release the `life` CLI for multiple architectures.

## Automated Releases via GitHub Actions

### Method 1: Tag-based Release (Recommended)

1. Create a semantic version tag:
   ```bash
   git tag v1.0.0
   git push origin v1.0.0
   ```

2. GitHub Actions automatically detects the tag and:
   - Builds binaries for all supported platforms
   - Generates SHA256 checksums
   - Creates a release on GitHub with all artifacts

### Method 2: Manual Workflow Dispatch

1. Go to the [Actions tab](https://github.com/integ-life/life-cli/actions) in GitHub
2. Select the "Release" workflow
3. Click "Run workflow"
4. Enter the version (e.g., `v1.0.0`)
5. Click "Run workflow"

## Local Building

To build binaries locally for all platforms:

```bash
./build.sh v1.0.0
```

This script:
- Builds binaries for all supported architectures
- Automatically disables CGO for cross-platform compilation (except on macOS)
- Generates SHA256 checksums in `build/checksums.txt`
- Strips and compresses binaries for minimal size

### Supported Platforms

- **linux/amd64** - Linux x86_64
- **linux/arm64** - Linux ARM64 (Raspberry Pi 4, etc.)
- **darwin/amd64** - macOS Intel
- **darwin/arm64** - macOS Apple Silicon (M1, M2, etc.)
- **windows/amd64** - Windows x86_64

## File Naming Convention

Binaries follow the naming pattern: `life-{VERSION}-{OS}-{ARCH}{EXT}`

Examples:
- `life-v1.0.0-linux-amd64`
- `life-v1.0.0-darwin-arm64`
- `life-v1.0.0-windows-amd64.exe`

## Verifying Downloads

SHA256 checksums are provided for each binary. After downloading:

```bash
# On macOS/Linux
sha256sum -c life-v1.0.0-linux-amd64.sha256

# Or use the combined checksums file
sha256sum -c checksums.txt
```

## Version Information

The binary includes version information that can be checked:

```bash
./life --version
# Output: life version v1.0.0
```

The version is set at compile time via the `-ldflags` flag in the build script.
