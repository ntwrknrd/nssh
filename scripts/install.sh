#!/bin/sh
set -e

# Configuration
REPO="ntwrknrd/nssh"
INSTALL_DIR="${HOME}/.local/bin"
BINARY="nssh"

# Colors (if terminal supports it)
if [ -t 1 ]; then
    RED='\033[0;31m'
    GREEN='\033[0;32m'
    YELLOW='\033[0;33m'
    BLUE='\033[0;34m'
    NC='\033[0m' # No Color
else
    RED=''
    GREEN=''
    YELLOW=''
    BLUE=''
    NC=''
fi

info() {
    printf "${BLUE}==>${NC} %s\n" "$1"
}

success() {
    printf "${GREEN}==>${NC} %s\n" "$1"
}

warn() {
    printf "${YELLOW}WARNING:${NC} %s\n" "$1"
}

error() {
    printf "${RED}ERROR:${NC} %s\n" "$1" >&2
    exit 1
}

# Detect OS
detect_os() {
    case "$(uname -s)" in
        Darwin) echo "darwin" ;;
        Linux)  echo "linux" ;;
        *)      error "Unsupported operating system: $(uname -s)" ;;
    esac
}

# Detect architecture
detect_arch() {
    case "$(uname -m)" in
        x86_64|amd64)   echo "amd64" ;;
        aarch64|arm64)  echo "arm64" ;;
        *)              error "Unsupported architecture: $(uname -m)" ;;
    esac
}

# Get latest version from GitHub API
get_latest_version() {
    if ! VERSION=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" 2>/dev/null | \
        grep '"tag_name"' | sed -E 's/.*"([^"]+)".*/\1/'); then
        error "Failed to fetch latest version from GitHub"
    fi
    if [ -z "${VERSION}" ]; then
        error "Could not determine latest version"
    fi
    echo "${VERSION}"
}

# Compute SHA256 checksum (works on both macOS and Linux)
sha256() {
    if command -v sha256sum >/dev/null 2>&1; then
        sha256sum "$1" | awk '{print $1}'
    elif command -v shasum >/dev/null 2>&1; then
        shasum -a 256 "$1" | awk '{print $1}'
    else
        error "No SHA256 utility found (need sha256sum or shasum)"
    fi
}

# Main install logic
main() {
    OS=$(detect_os)
    ARCH=$(detect_arch)

    info "Detecting platform: ${OS}/${ARCH}"

    VERSION=$(get_latest_version)
    VERSION_NUM=${VERSION#v}  # strip 'v' prefix

    info "Latest version: ${VERSION}"

    # Build URLs
    ARCHIVE="nssh_${VERSION_NUM}_${OS}_${ARCH}.tar.gz"
    BASE_URL="https://github.com/${REPO}/releases/download/${VERSION}"

    # Create temp directory
    TMP_DIR=$(mktemp -d)
    trap 'rm -rf "${TMP_DIR}"' EXIT

    # Download archive
    info "Downloading ${ARCHIVE}..."
    if ! curl -fsSL "${BASE_URL}/${ARCHIVE}" -o "${TMP_DIR}/${ARCHIVE}"; then
        error "Failed to download ${ARCHIVE}"
    fi

    # Download checksums
    info "Downloading checksums..."
    if ! curl -fsSL "${BASE_URL}/checksums.txt" -o "${TMP_DIR}/checksums.txt"; then
        error "Failed to download checksums.txt"
    fi

    # Verify checksum
    info "Verifying checksum..."
    cd "${TMP_DIR}"
    EXPECTED=$(grep "${ARCHIVE}" checksums.txt | awk '{print $1}')
    if [ -z "${EXPECTED}" ]; then
        error "Checksum not found for ${ARCHIVE}"
    fi

    ACTUAL=$(sha256 "${ARCHIVE}")
    if [ "${EXPECTED}" != "${ACTUAL}" ]; then
        error "Checksum verification failed!\n  Expected: ${EXPECTED}\n  Actual:   ${ACTUAL}"
    fi
    success "Checksum verified"

    # Extract and install
    info "Installing to ${INSTALL_DIR}..."
    mkdir -p "${INSTALL_DIR}"
    tar -xzf "${ARCHIVE}"
    mv "${BINARY}" "${INSTALL_DIR}/"
    chmod +x "${INSTALL_DIR}/${BINARY}"

    success "Installed ${BINARY} to ${INSTALL_DIR}/${BINARY}"

    # Check PATH
    case ":${PATH}:" in
        *":${INSTALL_DIR}:"*)
            ;;
        *)
            echo ""
            warn "${INSTALL_DIR} is not in your PATH"
            echo "    Add this to your shell profile (.bashrc, .zshrc, etc.):"
            echo ""
            echo "    export PATH=\"\${HOME}/.local/bin:\${PATH}\""
            echo ""
            ;;
    esac

    echo ""
    info "Run 'nssh self init' to set up shell integration"
}

main "$@"
