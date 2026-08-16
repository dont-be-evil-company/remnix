#!/bin/bash

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

REPO="dont-be-evil-company/remnix"
BINARY_NAME="remnix"

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

download_binary() {
    local version="$1"
    local platform="$2"
    local download_url="https://github.com/${REPO}/releases/download/${version}/${BINARY_NAME}-${platform}"

    if [[ "$platform" = *"windows"* ]]; then
        download_url="${download_url}.exe"
    fi

    local temp_dir
    temp_dir=$(mktemp -d)
    local binary_path="${temp_dir}/${BINARY_NAME}"

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
        rm -rf "$temp_dir"
        exit 1
    fi

    if [[ "$platform" != *"windows"* ]]; then
        chmod +x "$binary_path"
    fi

    echo "$binary_path"
}

get_install_location() {
    if [ "$(id -u)" -eq 0 ]; then
        echo "/usr/local/bin/${BINARY_NAME}"
    else
        local user_bin="${HOME}/.local/bin"
        mkdir -p "$user_bin"
        echo "${user_bin}/${BINARY_NAME}"
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

    print_status "Installing ${BINARY_NAME} to ${install_path}..."

    if [ -f "$install_path" ]; then
        local backup_path
        backup_path="${install_path}.backup.$(date +%Y%m%d_%H%M%S)"
        print_warning "Backing up existing binary to ${backup_path}"
        cp "$install_path" "$backup_path"
    fi

    if cp "$source_path" "$install_path"; then
        print_success "${BINARY_NAME} installed successfully to ${install_path}"

        if [[ "$install_path" == *"/.local/bin"* ]]; then
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
    else
        print_error "Failed to install ${BINARY_NAME}"
        exit 1
    fi
}

verify_installation() {
    local install_path="$1"

    if [ -f "$install_path" ] && [ -x "$install_path" ]; then
        print_success "Installation verified successfully!"
        print_status "You can now run: ${BINARY_NAME} --version"
    else
        print_error "Installation verification failed"
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

    print_status "Downloading ${BINARY_NAME} ${version} for ${platform}..."
    local temp_binary
    temp_binary=$(download_binary "$version" "$platform")

    local install_path
    install_path=$(get_install_location)

    install_binary "$temp_binary" "$install_path"
    rm -rf "$(dirname "$temp_binary")"
    verify_installation "$install_path"

    print_success "${BINARY_NAME} installation completed successfully!"
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
