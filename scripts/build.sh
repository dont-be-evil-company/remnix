#!/usr/bin/env bash
set -euo pipefail

if [ -z "${VERSION:-}" ]; then
  echo "Error: VERSION is not set"
  exit 1
fi

build_wrapper() {
  echo "Building for $1 $2"
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

build_linux_x86_64() {
  build_wrapper "linux" "amd64"
}

build_linux_arm64() {
  build_wrapper "linux" "arm64"
}

build_linux() {
  build_linux_x86_64
}

build_macos_arm64() {
  build_wrapper "darwin" "arm64"
}

build_macos_x86_64() {
  build_wrapper "darwin" "amd64"
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
