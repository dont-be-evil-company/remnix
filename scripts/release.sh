#!/usr/bin/env bash
set -euo pipefail

if [ -z "${VERSION:-}" ]; then
  echo "Error: VERSION is not set"
  exit 1
fi

GH_TAG="v$VERSION"

FILES=(
  "dist/remnix-linux-amd64"
  "dist/remnix-linux-arm64"
  "dist/remnix-attach-linux-amd64"
  "dist/remnix-attach-linux-arm64"
  "dist/remnix-darwin-amd64"
  "dist/remnix-darwin-arm64"
  "dist/remnix-attach-darwin-amd64"
  "dist/remnix-attach-darwin-arm64"
  "dist/remnix-windows-amd64.exe"
)

for file in "${FILES[@]}"; do
  if [ ! -f "$file" ]; then
    echo "Error: missing release artifact: $file"
    ls -l dist/ || true
    exit 1
  fi
done

(
  cd dist
  sha256sum * > SHA256SUMS
)

echo "Creating new release $GH_TAG"
echo "Files to upload:"
for file in "${FILES[@]}" dist/SHA256SUMS; do
  printf " - %s\n" "$file"
done

gh release create --generate-notes "$GH_TAG" "${FILES[@]}" dist/SHA256SUMS
