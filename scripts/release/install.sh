#!/bin/sh
# MCPer install script
# Usage: curl -sSL https://storage.googleapis.com/mcper-releases/install.sh | sh
#    or: curl -sSL https://storage.googleapis.com/mcper-releases/install.sh | sh -s -- <version>
set -eu

# Configuration
GCS_BUCKET="https://storage.googleapis.com/mcper-releases"
INSTALL_DIR="${MCPER_INSTALL_DIR:-$HOME/.mcper/bin}"
DEFAULT_VERSION="latest"

info() {
    printf '\033[0;32m[mcper]\033[0m %s\n' "$1"
}

warn() {
    printf '\033[1;33m[mcper]\033[0m %s\n' "$1" >&2
}

error() {
    printf '\033[0;31m[mcper]\033[0m %s\n' "$1" >&2
    exit 1
}

# Detect OS and architecture
detect_platform() {
    os=""
    arch=""

    case "$(uname -s)" in
        Linux*)  os="linux" ;;
        Darwin*) os="darwin" ;;
        MINGW*|MSYS*|CYGWIN*) os="windows" ;;
        *)       error "Unsupported operating system: $(uname -s)" ;;
    esac

    case "$(uname -m)" in
        x86_64|amd64)  arch="amd64" ;;
        arm64|aarch64) arch="arm64" ;;
        *)             error "Unsupported architecture: $(uname -m)" ;;
    esac

    printf '%s-%s' "$os" "$arch"
}

# Get version to install (fetches from GCS latest.json if "latest")
get_version() {
    version="${1:-$DEFAULT_VERSION}"

    if [ "$version" = "latest" ]; then
        # Fetch latest version from GCS latest.json
        latest_url="${GCS_BUCKET}/latest.json"
        version=$(curl -sSL "$latest_url" 2>/dev/null | grep '"version"' | sed -E 's/.*"version"[[:space:]]*:[[:space:]]*"([^"]+)".*/\1/')

        if [ -z "$version" ]; then
            error "Failed to fetch latest version from ${latest_url}"
        fi
    fi

    printf '%s' "$version"
}

# Download and install mcper
install_mcper() {
    version="$1"
    platform="$2"

    binary_name="mcper"
    asset_name="mcper-${platform}"
    case "$platform" in
        windows-*)
            binary_name="mcper.exe"
            asset_name="mcper-${platform}.exe"
            ;;
    esac

    download_url="${GCS_BUCKET}/v${version}/${asset_name}"

    info "Downloading mcper (${version}) for ${platform}..."
    info "URL: $download_url"

    # Create install directory
    mkdir -p "$INSTALL_DIR"

    # Download binary
    tmp_file=$(mktemp)
    if ! curl -sSL -o "$tmp_file" "$download_url"; then
        rm -f "$tmp_file"
        error "Failed to download mcper from $download_url"
    fi

    # Check if download was successful (not a 404 HTML page)
    if file "$tmp_file" | grep -q "HTML"; then
        rm -f "$tmp_file"
        error "Failed to download mcper - release not found. Check https://github.com/joshcarp/mcper/releases"
    fi

    # Make executable and move to install dir
    chmod +x "$tmp_file"
    mv "$tmp_file" "${INSTALL_DIR}/${binary_name}"

    info "Installed mcper to ${INSTALL_DIR}/${binary_name}"
}

# Add to PATH
setup_path() {
    path_line="export PATH=\"\$PATH:$INSTALL_DIR\""

    # Check if already in PATH
    case ":$PATH:" in
        *":$INSTALL_DIR:"*)
            info "mcper is already in PATH"
            return
            ;;
    esac

    # Add to current session
    PATH="$PATH:$INSTALL_DIR"
    export PATH

    # Detect and update all common shell config files
    configs_updated=0

    # Bash configs
    for rc in "$HOME/.bashrc" "$HOME/.bash_profile" "$HOME/.profile"; do
        if [ -f "$rc" ] && [ -w "$rc" ]; then
            if ! grep -q "$INSTALL_DIR" "$rc" 2>/dev/null; then
                echo "" >> "$rc" 2>/dev/null && \
                echo "# Added by mcper installer" >> "$rc" && \
                echo "$path_line" >> "$rc" && \
                info "Added mcper to $rc" && \
                configs_updated=$((configs_updated + 1))
            fi
        fi
    done

    # Zsh config
    if [ -f "$HOME/.zshrc" ] && [ -w "$HOME/.zshrc" ]; then
        if ! grep -q "$INSTALL_DIR" "$HOME/.zshrc" 2>/dev/null; then
            echo "" >> "$HOME/.zshrc" 2>/dev/null && \
            echo "# Added by mcper installer" >> "$HOME/.zshrc" && \
            echo "$path_line" >> "$HOME/.zshrc" && \
            info "Added mcper to ~/.zshrc" && \
            configs_updated=$((configs_updated + 1))
        fi
    fi

    # If no config files updated, try to create/update .zshrc (common on macOS)
    if [ "$configs_updated" -eq 0 ]; then
        if touch "$HOME/.zshrc" 2>/dev/null; then
            echo "" >> "$HOME/.zshrc"
            echo "# Added by mcper installer" >> "$HOME/.zshrc"
            echo "$path_line" >> "$HOME/.zshrc"
            info "Added mcper to ~/.zshrc"
        else
            warn "Could not update shell config. Add this to your shell config manually:"
            warn "  $path_line"
        fi
    fi

    info ""
    info "Restart your terminal or run: source ~/.zshrc (or ~/.bashrc)"
}

# Verify installation
verify_install() {
    if ! "${INSTALL_DIR}/mcper" version >/dev/null 2>&1; then
        error "Installation verification failed"
    fi

    info "Successfully installed mcper!"
    info ""
    info "Run 'mcper --help' to get started"
}

main() {
    requested_version="${1:-$DEFAULT_VERSION}"

    info "Installing mcper..."

    platform=$(detect_platform)
    info "Detected platform: $platform"

    # Resolve "latest" to actual version number
    version=$(get_version "$requested_version")
    info "Version: $version"

    install_mcper "$version" "$platform"
    setup_path
    verify_install
}

main "$@"
