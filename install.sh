#!/usr/bin/env sh
set -eu

YC1_REPO="${YC1_REPO:-yingca1/yc1}"
YC1_VERSION="${YC1_VERSION:-latest}"
YC1_INSTALL_DIR="${YC1_INSTALL_DIR:-$HOME/.local/bin}"

detect_os() {
  case "$(uname -s)" in
    Darwin) printf '%s\n' darwin ;;
    Linux) printf '%s\n' linux ;;
    *) printf 'unsupported OS: %s\n' "$(uname -s)" >&2; exit 1 ;;
  esac
}

detect_arch() {
  case "$(uname -m)" in
    x86_64 | amd64) printf '%s\n' amd64 ;;
    arm64 | aarch64) printf '%s\n' arm64 ;;
    *) printf 'unsupported architecture: %s\n' "$(uname -m)" >&2; exit 1 ;;
  esac
}

sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

os="$(detect_os)"
arch="$(detect_arch)"
asset="yc1_${os}_${arch}.tar.gz"

if [ "$YC1_VERSION" = "latest" ]; then
  base_url="https://github.com/${YC1_REPO}/releases/latest/download"
else
  base_url="https://github.com/${YC1_REPO}/releases/download/${YC1_VERSION}"
fi

tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT INT TERM

archive="$tmp_dir/$asset"
checksum="$tmp_dir/$asset.sha256"

curl -fsSL "$base_url/$asset" -o "$archive"
curl -fsSL "$base_url/$asset.sha256" -o "$checksum"

expected="$(awk '{print $1}' "$checksum")"
actual="$(sha256_file "$archive")"
if [ "$expected" != "$actual" ]; then
  printf 'yc1: checksum mismatch for %s\n' "$asset" >&2
  exit 1
fi

tar -xzf "$archive" -C "$tmp_dir"
mkdir -p "$YC1_INSTALL_DIR"
install -m 0755 "$tmp_dir/yc1" "$YC1_INSTALL_DIR/yc1"

printf 'yc1 installed to %s\n' "$YC1_INSTALL_DIR/yc1"
case ":$PATH:" in
  *":$YC1_INSTALL_DIR:"*) ;;
  *) printf 'Add %s to PATH to run yc1 from any shell.\n' "$YC1_INSTALL_DIR" ;;
esac
