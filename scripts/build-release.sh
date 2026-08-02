#!/usr/bin/env bash
set -euo pipefail

version=${1:-}
output_dir=${2:-dist}

if [[ ! "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$ ]]; then
  echo "usage: $0 vMAJOR.MINOR.PATCH [OUTPUT_DIR]" >&2
  exit 2
fi

project_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
output_dir=$(mkdir -p -- "$output_dir" && cd -- "$output_dir" && pwd)
work_dir=$(mktemp -d)
trap 'rm -rf -- "$work_dir"' EXIT

targets=(
  "linux amd64"
  "linux arm64"
  "darwin amd64"
  "darwin arm64"
  "windows amd64"
  "windows arm64"
)

for target in "${targets[@]}"; do
  read -r target_os target_arch <<<"$target"
  archive="mailtui_${target_os}_${target_arch}"
  binary="mailtui"
  if [[ "$target_os" == "windows" ]]; then
    binary="mailtui.exe"
  fi

  package_dir="$work_dir/$archive"
  mkdir -p -- "$package_dir"
  (
    cd -- "$project_root"
    CGO_ENABLED=0 GOOS="$target_os" GOARCH="$target_arch" \
      go build -trimpath -ldflags="-s -w -X main.version=$version" \
      -o "$package_dir/$binary" .
  )

  if [[ "$target_os" == "windows" ]]; then
    (cd -- "$package_dir" && zip -q "$output_dir/$archive.zip" "$binary")
  else
    tar -C "$package_dir" -czf "$output_dir/$archive.tar.gz" "$binary"
  fi
done

(
  cd -- "$output_dir"
  sha256sum mailtui_* > checksums.txt
)

echo "Release assets written to $output_dir"
