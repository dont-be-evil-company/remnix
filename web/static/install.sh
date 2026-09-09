#!/bin/bash

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

REPO="dont-be-evil-company/remnix"
BINARY_NAME="remnix"
ATTACH_NAME="remnix-attach"

print_status() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

print_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

detect_platform() {
    local os
    local arch

    case "$(uname -s)" in
        Linux*)     os="linux" ;;
        Darwin*)    os="darwin" ;;
        CYGWIN*|MINGW*|MSYS*) os="windows" ;;
        *)          print_error "Unsupported operating system: $(uname -s)"; exit 1 ;;
    esac

    case "$(uname -m)" in
        x86_64|amd64)  arch="amd64" ;;
        arm64|aarch64) arch="arm64" ;;
        armv7l)        arch="armv7" ;;
        *)             print_error "Unsupported architecture: $(uname -m)"; exit 1 ;;
    esac

    echo "${os}-${arch}"
}

get_latest_version() {
    local redirect_url

    if command -v curl >/dev/null 2>&1; then
        redirect_url=$(curl -s -I "https://github.com/${REPO}/releases/latest" | grep -i "location:" | sed 's/.*\/releases\/tag\///' | tr -d '\r')
    elif command -v wget >/dev/null 2>&1; then
        redirect_url=$(wget --spider -S "https://github.com/${REPO}/releases/latest" 2>&1 | grep -i "location:" | sed 's/.*\/releases\/tag\///' | tr -d '\r')
    else
        print_error "Neither curl nor wget is available. Please install one of them."
        exit 1
    fi

    if [ -z "$redirect_url" ]; then
        print_error "Failed to get latest version from GitHub"
        exit 1
    fi

    echo "$redirect_url"
}

download_asset() {
    local version="$1"
    local platform="$2"
    local name="$3"
    local dest_dir="$4"
    local download_url="https://github.com/${REPO}/releases/download/${version}/${name}-${platform}"
    local binary_path="${dest_dir}/${name}"

    if [[ "$platform" = *"windows"* ]]; then
        download_url="${download_url}.exe"
        binary_path="${binary_path}.exe"
    fi

    local download_success=false

    if command -v curl >/dev/null 2>&1; then
        if curl -L -o "$binary_path" "$download_url"; then
            download_success=true
        fi
    elif command -v wget >/dev/null 2>&1; then
        if wget -O "$binary_path" "$download_url"; then
            download_success=true
        fi
    fi

    if [ "$download_success" = false ]; then
        print_error "Failed to download binary from ${download_url}"
        return 1
    fi

    if [[ "$platform" != *"windows"* ]]; then
        chmod +x "$binary_path"
    fi

    echo "$binary_path"
}

get_install_dir() {
    if [ "$(id -u)" -eq 0 ]; then
        echo "/usr/local/bin"
    else
        local user_bin="${HOME}/.local/bin"
        mkdir -p "$user_bin"
        echo "$user_bin"
    fi
}

detect_shell_rc() {
    local shell_rc

    if [ -n "$ZSH_VERSION" ]; then
        shell_rc="${HOME}/.zshrc"
    elif [ -n "$BASH_VERSION" ]; then
        shell_rc="${HOME}/.bashrc"
    elif [ -n "$SHELL" ]; then
        case "$SHELL" in
            */zsh)  shell_rc="${HOME}/.zshrc" ;;
            */bash) shell_rc="${HOME}/.bashrc" ;;
            *)      shell_rc="${HOME}/.profile" ;;
        esac
    else
        shell_rc="${HOME}/.profile"
    fi

    echo "$shell_rc"
}

install_binary() {
    local source_path="$1"
    local install_path="$2"
    local label="$3"
    local staged_path="${install_path}.new"

    print_status "Installing ${label} to ${install_path}..."

    if [ -f "$install_path" ]; then
        local backup_path
        backup_path="${install_path}.backup.$(date +%Y%m%d_%H%M%S)"
        print_warning "Backing up existing binary to ${backup_path}"
        cp "$install_path" "$backup_path"
    fi

    # Stage + rename so a running binary (ETXTBSY) can still be replaced.
    if cp "$source_path" "$staged_path" && chmod +x "$staged_path" && mv -f "$staged_path" "$install_path"; then
        print_success "${label} installed successfully to ${install_path}"
    else
        rm -f "$staged_path"
        print_error "Failed to install ${label}"
        exit 1
    fi
}

ensure_path() {
    local install_dir="$1"

    if [[ "$install_dir" == *"/.local/bin"* ]]; then
        local shell_rc
        shell_rc=$(detect_shell_rc)
        if ! grep -q "\.local/bin" "$shell_rc" 2>/dev/null; then
            print_status "Adding ${HOME}/.local/bin to PATH in ${shell_rc}"
            {
              echo ""
              echo "# Add local bin directory to PATH"
              echo "export PATH=$HOME/.local/bin:$PATH"
            } >> "$shell_rc"
            print_warning "Please restart your shell or run 'source ${shell_rc}' to update PATH"
        fi
    fi
}

verify_installation() {
    local install_path="$1"
    local label="$2"

    if [ -f "$install_path" ] && [ -x "$install_path" ]; then
        print_success "${label} installation verified"
    else
        print_error "${label} installation verification failed"
        exit 1
    fi
}

main() {
    print_status "Installing ${BINARY_NAME}..."

    local platform
    platform=$(detect_platform)
    print_status "Detected platform: ${platform}"

    local version
    version=$(get_latest_version)
    print_status "Latest version: ${version}"

    local temp_dir
    temp_dir=$(mktemp -d)

    print_status "Downloading ${BINARY_NAME} ${version} for ${platform}..."
    local temp_binary
    if ! temp_binary=$(download_asset "$version" "$platform" "$BINARY_NAME" "$temp_dir"); then
        rm -rf "$temp_dir"
        exit 1
    fi

    print_status "Downloading ${ATTACH_NAME} ${version} for ${platform}..."
    local temp_attach
    if ! temp_attach=$(download_asset "$version" "$platform" "$ATTACH_NAME" "$temp_dir"); then
        rm -rf "$temp_dir"
        exit 1
    fi

    local install_dir
    install_dir=$(get_install_dir)
    local install_path="${install_dir}/${BINARY_NAME}"
    local attach_path="${install_dir}/${ATTACH_NAME}"

    install_binary "$temp_binary" "$install_path" "$BINARY_NAME"
    install_binary "$temp_attach" "$attach_path" "$ATTACH_NAME"
    ensure_path "$install_dir"

    rm -rf "$temp_dir"
    verify_installation "$install_path" "$BINARY_NAME"
    verify_installation "$attach_path" "$ATTACH_NAME"

    print_success "${BINARY_NAME} installation completed successfully!"
    print_status "You can now run: ${BINARY_NAME} --version"
}

if [[ "$(uname -s)" == *"MINGW"* ]] || [[ "$(uname -s)" == *"MSYS"* ]]; then
    print_error "This script is designed for Unix-like systems (Linux/macOS)"
    print_error "For Windows, use: iwr /install.ps1 -useb | iex"
    exit 1
fi

if ! command -v curl >/dev/null 2>&1 && ! command -v wget >/dev/null 2>&1; then
    print_error "Either curl or wget is required but neither is installed."
    exit 1
fi

main "$@"
