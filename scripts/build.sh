#!/usr/bin/env bash
set -euo pipefail

if [ -z "${VERSION:-}" ]; then
  echo "Error: VERSION is not set"
  exit 1
fi

host_arch="$(uname -m)"
case "$host_arch" in
  x86_64|amd64) host_arch="amd64" ;;
  aarch64|arm64) host_arch="arm64" ;;
esac

build_wrapper() {
  echo "Building remnix for $1 $2"
  local windows_file_extension=""
  if [ "$1" == "windows" ]; then
    windows_file_extension=".exe"
  fi
  local output="dist/remnix-$1-$2$windows_file_extension"
  CGO_ENABLED=0 GOOS="$1" GOARCH="$2" go build \
    -ldflags "-s -w -X github.com/dont-be-evil-company/remnix/internal/version.Version=${VERSION}" \
    -o "$output" \
    ./cmd/remnix
}

# remnix-attach is Odin and cannot be cross-linked on Linux. On macOS the
# Apple linker usually supports both darwin arches from either host.
build_attach() {
  local os="$1"
  local arch="$2"

  if [ "$os" = "windows" ]; then
    echo "Skipping remnix-attach for windows (not supported)"
    return 0
  fi

  if ! command -v odin >/dev/null 2>&1; then
    echo "Error: odin is required to build remnix-attach"
    exit 1
  fi

  if [ "$os" = "linux" ] && [ "$arch" != "$host_arch" ]; then
    echo "Skipping remnix-attach-linux-${arch} (Odin cannot cross-link; build on a ${arch} runner)"
    return 0
  fi

  local odin_target="${os}_${arch}"
  local output="dist/remnix-attach-${os}-${arch}"
  echo "Building remnix-attach for ${os} ${arch} (-target:${odin_target})"
  odin build cmd/remnix-attach -out:"${output}" -o:speed -target:"${odin_target}"
}

build_linux_x86_64() {
  build_wrapper "linux" "amd64"
  build_attach "linux" "amd64"
}

build_linux_arm64() {
  build_wrapper "linux" "arm64"
  build_attach "linux" "arm64"
}

build_linux() {
  build_linux_x86_64
}

build_macos_arm64() {
  build_wrapper "darwin" "arm64"
  build_attach "darwin" "arm64"
}

build_macos_x86_64() {
  build_wrapper "darwin" "amd64"
  build_attach "darwin" "amd64"
}

build_macos() {
  build_macos_arm64
  build_macos_x86_64
}

build_windows_x86_64() {
  build_wrapper "windows" "amd64"
}

build_windows() {
  build_windows_x86_64
}

mkdir -p dist

case "${TARGET_PLATFORM}" in
  "linux")
    build_linux
    ;;
  "linux-arm64")
    build_linux_arm64
    ;;
  "linux-arm64-attach")
    # Native arm64 runner: produce remnix-attach-linux-arm64 only.
    build_attach "linux" "arm64"
    ;;
  "macos")
    build_macos
    ;;
  "windows")
    build_windows
    ;;
  *)
    echo "Error: TARGET_PLATFORM ${TARGET_PLATFORM} is not supported"
    exit 1
    ;;
esac
