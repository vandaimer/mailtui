#!/usr/bin/env sh
set -eu

repository=${MAILTUI_REPO:-}
version=${MAILTUI_VERSION:-latest}
install_dir=${MAILTUI_INSTALL_DIR:-"${HOME}/.local/bin"}

usage() {
  cat <<'EOF'
Install a prebuilt mailtui release from GitHub.

Usage:
  install.sh --repo OWNER/REPOSITORY [--version vX.Y.Z] [--dir PATH]

Environment variables:
  MAILTUI_REPO         GitHub repository in OWNER/REPOSITORY form
  MAILTUI_VERSION      Release tag, or "latest" (default)
  MAILTUI_INSTALL_DIR  Destination directory (default: ~/.local/bin)
EOF
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --repo)
      repository=${2:?missing value for --repo}
      shift 2
      ;;
    --version)
      version=${2:?missing value for --version}
      shift 2
      ;;
    --dir)
      install_dir=${2:?missing value for --dir}
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

case "$repository" in
  */*) ;;
  *)
    echo "--repo OWNER/REPOSITORY is required" >&2
    exit 2
    ;;
esac

case $(uname -s) in
  Linux) target_os=linux ;;
  Darwin) target_os=darwin ;;
  *)
    echo "unsupported operating system: $(uname -s)" >&2
    exit 1
    ;;
esac

case $(uname -m) in
  x86_64|amd64) target_arch=amd64 ;;
  arm64|aarch64) target_arch=arm64 ;;
  *)
    echo "unsupported architecture: $(uname -m)" >&2
    exit 1
    ;;
esac

asset="mailtui_${target_os}_${target_arch}.tar.gz"
if [ "$version" = latest ]; then
  base_url="https://github.com/${repository}/releases/latest/download"
else
  base_url="https://github.com/${repository}/releases/download/${version}"
fi

temporary_dir=$(mktemp -d)
trap 'rm -rf "$temporary_dir"' EXIT HUP INT TERM

download() {
  source_url=$1
  destination=$2
  if command -v curl >/dev/null 2>&1; then
    curl --fail --location --silent --show-error --output "$destination" "$source_url"
  elif command -v wget >/dev/null 2>&1; then
    wget --quiet --output-document="$destination" "$source_url"
  else
    echo "curl or wget is required" >&2
    exit 1
  fi
}

download "$base_url/$asset" "$temporary_dir/$asset"
download "$base_url/checksums.txt" "$temporary_dir/checksums.txt"

expected=$(awk -v asset="$asset" '$2 == asset { print $1 }' "$temporary_dir/checksums.txt")
if [ -z "$expected" ]; then
  echo "checksum for $asset was not found" >&2
  exit 1
fi

if command -v sha256sum >/dev/null 2>&1; then
  actual=$(sha256sum "$temporary_dir/$asset" | awk '{ print $1 }')
else
  actual=$(shasum -a 256 "$temporary_dir/$asset" | awk '{ print $1 }')
fi

if [ "$actual" != "$expected" ]; then
  echo "checksum verification failed for $asset" >&2
  exit 1
fi

tar -C "$temporary_dir" -xzf "$temporary_dir/$asset"
mkdir -p "$install_dir"
install -m 0755 "$temporary_dir/mailtui" "$install_dir/mailtui"

echo "mailtui installed to $install_dir/mailtui"
case ":${PATH}:" in
  *":${install_dir}:"*) ;;
  *) echo "Add $install_dir to PATH to run mailtui from any directory." ;;
esac
