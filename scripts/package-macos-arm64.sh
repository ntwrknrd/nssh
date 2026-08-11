#!/bin/sh
set -eu

usage() {
  cat <<'EOF'
Usage: scripts/package-macos-arm64.sh [--version VERSION] [--output-dir DIR]

Build a copyable macOS Apple Silicon nssh package.

Options:
  --version VERSION  Version string embedded in the binary and artifact name.
                     Defaults to $VERSION or git describe.
  --output-dir DIR   Directory for the package and checksum.
                     Defaults to ~/Downloads.
  -h, --help         Show this help.

The old positional output directory form is still accepted:
  scripts/package-macos-arm64.sh ~/Downloads
EOF
}

OUT_ROOT="${PACKAGE_DIR:-${HOME}/Downloads}"
VERSION="${VERSION:-}"

while [ "$#" -gt 0 ]; do
  case "$1" in
    --version)
      if [ "$#" -lt 2 ]; then
        printf '%s\n' "error: --version requires a value" >&2
        exit 2
      fi
      VERSION="$2"
      shift 2
      ;;
    --output-dir)
      if [ "$#" -lt 2 ]; then
        printf '%s\n' "error: --output-dir requires a value" >&2
        exit 2
      fi
      OUT_ROOT="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    --*)
      printf 'error: unknown option: %s\n' "$1" >&2
      usage >&2
      exit 2
      ;;
    *)
      if [ "${OUT_ROOT_SET:-0}" -eq 1 ]; then
        printf 'error: unexpected argument: %s\n' "$1" >&2
        usage >&2
        exit 2
      fi
      OUT_ROOT="$1"
      OUT_ROOT_SET=1
      shift
      ;;
  esac
done

if [ -z "${VERSION}" ]; then
  VERSION="$(git describe --tags --always --dirty 2>/dev/null || echo dev)"
fi

COMMIT="$(git rev-parse --short HEAD 2>/dev/null || echo unknown)"
BUILT_AT="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
PACKAGE_NAME="nssh-${VERSION}-macos-arm64"
PACKAGE_DIR="${OUT_ROOT}/${PACKAGE_NAME}"
ARCHIVE="${OUT_ROOT}/${PACKAGE_NAME}.tar.gz"
CHECKSUM="${ARCHIVE}.sha256"

mkdir -p "${PACKAGE_DIR}"

GOOS=darwin GOARCH=arm64 go build \
  -trimpath \
  -buildvcs=false \
  -ldflags "-s -w -X main.version=${VERSION}" \
  -o "${PACKAGE_DIR}/nssh" \
  ./cmd/nssh

GOOS=darwin GOARCH=arm64 go build \
  -trimpath \
  -buildvcs=false \
  -ldflags "-s -w" \
  -o "${PACKAGE_DIR}/nssh-askpass" \
  ./cmd/nssh-askpass

if command -v codesign >/dev/null 2>&1; then
  codesign -s - "${PACKAGE_DIR}/nssh" >/dev/null
  codesign -s - "${PACKAGE_DIR}/nssh-askpass" >/dev/null
fi

cat > "${PACKAGE_DIR}/BUILD.txt" <<EOF
nssh ${VERSION}
commit: ${COMMIT}
built: ${BUILT_AT}
platform: macOS darwin/arm64
source: $(pwd)

Install:
  install -m 0755 nssh ~/.local/bin/nssh
  install -m 0755 nssh-askpass ~/.local/bin/nssh-askpass

If macOS quarantine blocks execution after copying:
  xattr -d com.apple.quarantine ~/.local/bin/nssh
  xattr -d com.apple.quarantine ~/.local/bin/nssh-askpass
EOF

(
  cd "${OUT_ROOT}"
  tar -czf "${ARCHIVE}" "${PACKAGE_NAME}"
  shasum -a 256 "${PACKAGE_NAME}.tar.gz" > "${CHECKSUM}"
)

"${PACKAGE_DIR}/nssh" -V
file "${PACKAGE_DIR}/nssh"
file "${PACKAGE_DIR}/nssh-askpass"
printf 'package: %s\n' "${ARCHIVE}"
printf 'sha256:  %s\n' "${CHECKSUM}"
