#!/bin/sh
set -e

# Configuration
REPO="ntwrknrd/nssh"
INSTALL_DIR="${HOME}/.local/bin"
BINARY="nssh"
RELEASE=""
EVENTS=0

# Colors (if terminal supports it)
if [ -t 1 ]; then
    ESC=$(printf '\033')
    DIM="${ESC}[2m"
    RED="${ESC}[0;31m"
    GREEN="${ESC}[0;32m"
    YELLOW="${ESC}[0;33m"
    GRAY="${ESC}[0;90m"
    NC="${ESC}[0m"
else
    DIM=''
    RED=''
    GREEN=''
    YELLOW=''
    GRAY=''
    NC=''
fi

info() {
    printf "  ${DIM}${GRAY}[*]${NC} %s\n" "$1"
}

success() {
    printf "  ${DIM}${GREEN}[✓]${NC} %s\n" "$1"
}

warn() {
    printf "  ${DIM}${YELLOW}[!]${NC} %s\n" "$1"
}

error() {
    printf "  ${DIM}${RED}[✗]${NC} %s\n" "$1" >&2
    exit 1
}

status() {
    if [ "${EVENTS}" -eq 1 ]; then
        printf 'NSSH_INSTALL_STATUS\t%s\n' "$1"
    else
        info "$1"
    fi
}

event() {
    if [ "${EVENTS}" -eq 1 ]; then
        printf 'NSSH_INSTALL_%s\t%s\n' "$1" "$2"
    fi
}

# Parse arguments
while [ "$#" -gt 0 ]; do
    case "$1" in
        --events)
            EVENTS=1
            shift
            ;;
        --release)
            if [ "$#" -lt 2 ]; then
                error "--release requires a value"
            fi
            RELEASE="$2"
            shift 2
            ;;
        *)
            error "Unknown option: $1"
            ;;
    esac
done

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

normalize_version() {
    VERSION="$1"
    case "${VERSION}" in
        *[!A-Za-z0-9._-]*|'')
            error "Invalid release tag: ${VERSION}"
            ;;
    esac
    case "${VERSION}" in
        [0-9]*)
            VERSION="v${VERSION}"
            ;;
    esac
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
    status "Detecting platform"
    OS=$(detect_os)
    ARCH=$(detect_arch)

    if [ -n "${RELEASE}" ]; then
        status "Selecting version"
        VERSION=$(normalize_version "${RELEASE}")
    else
        status "Fetching latest release"
        VERSION=$(get_latest_version)
    fi
    VERSION_NUM=${VERSION#v}  # strip 'v' prefix
    event "VERSION" "${VERSION}"

    if [ "${EVENTS}" -eq 0 ]; then
        info "Detecting platform: ${OS}/${ARCH}"
        if [ -n "${RELEASE}" ]; then
            info "Selected version: ${VERSION}"
        else
            info "Latest version: ${VERSION}"
        fi
    fi

    ARCHIVE="nssh_${VERSION_NUM}_${OS}_${ARCH}.tar.gz"
    ARCHIVE_BINARY="nssh"
    BASE_URL="https://github.com/${REPO}/releases/download/${VERSION}"

    # Create temp directory
    TMP_DIR=$(mktemp -d)
    trap 'rm -rf "${TMP_DIR}"' EXIT

    # Download archive
    status "Downloading ${ARCHIVE}"
    if ! curl -fsSL "${BASE_URL}/${ARCHIVE}" -o "${TMP_DIR}/${ARCHIVE}"; then
        error "Failed to download ${ARCHIVE}"
    fi

    # Download checksums
    status "Downloading checksums"
    if ! curl -fsSL "${BASE_URL}/checksums.txt" -o "${TMP_DIR}/checksums.txt"; then
        error "Failed to download checksums.txt"
    fi

    # Verify checksum
    status "Verifying checksum"
    cd "${TMP_DIR}"
    EXPECTED=$(grep "${ARCHIVE}" checksums.txt | awk '{print $1}')
    if [ -z "${EXPECTED}" ]; then
        error "Checksum not found for ${ARCHIVE}"
    fi

    ACTUAL=$(sha256 "${ARCHIVE}")
    if [ "${EXPECTED}" != "${ACTUAL}" ]; then
        error "Checksum verification failed!\n  Expected: ${EXPECTED}\n  Actual:   ${ACTUAL}"
    fi
    if [ "${EVENTS}" -eq 0 ]; then
        success "Checksum verified"
    fi

    # Extract and install
    status "Installing"
    mkdir -p "${INSTALL_DIR}"
    tar -xzf "${ARCHIVE}"
    mv "${ARCHIVE_BINARY}" "${INSTALL_DIR}/${BINARY}"
    chmod +x "${INSTALL_DIR}/${BINARY}"

    event "PATH" "${INSTALL_DIR}/${BINARY}"
    if [ "${EVENTS}" -eq 0 ]; then
        success "Installed ${BINARY} to ${INSTALL_DIR}/${BINARY}"
    fi

    # Check PATH
    case ":${PATH}:" in
        *":${INSTALL_DIR}:"*)
            ;;
        *)
            warn "${INSTALL_DIR} is not in your PATH"
            printf "      Add to shell profile: ${GRAY}export PATH=\"\${HOME}/.local/bin:\${PATH}\"${NC}\n"
            ;;
    esac

    if [ "${EVENTS}" -eq 0 ]; then
        info "Run 'nssh self init' to set up nssh"
    fi
}

main "$@"
