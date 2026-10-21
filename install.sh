#!/usr/bin/env sh
# install.sh — single-file install script for kabby.
# Detects OS/arch, downloads the matching static binary from GitHub releases,
# and installs it to /usr/local/bin (or $HOME/.local/bin if no write access).
#
# One-liner to run:
#   curl -fsSL https://raw.githubusercontent.com/vicgupta/kabby/main/install.sh | sh
set -eu

VERSION="${KABBY_VERSION:-v0.8.2}"
REPO="vicgupta/kabby"

# Detect OS
os="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$os" in
  linux)   os="linux" ;;
  darwin)  os="darwin" ;;
  msys*|mingw*|cygwin*) os="windows" ;;
  *)
    echo "Unsupported OS: $os" >&2
    exit 1
    ;;
esac

# Detect architecture
arch="$(uname -m)"
case "$arch" in
  x86_64|amd64) arch="amd64" ;;
  aarch64|arm64) arch="arm64" ;;
  *)
    echo "Unsupported architecture: $arch" >&2
    exit 1
    ;;
esac

# Determine binary name
if [ "$os" = "windows" ]; then
  bin="kabby-${os}-${arch}.exe"
else
  bin="kabby-${os}-${arch}"
fi

url="https://github.com/${REPO}/releases/download/${VERSION}/${bin}"
target="/usr/local/bin/kabby"
if [ ! -w "$(dirname "$target")" ]; then
  target="${HOME}/.local/bin/kabby"
  mkdir -p "$(dirname "$target")"
fi

echo "Downloading ${bin} -> ${target} ..."
# Download next to the target, then rename over it. Writing into the
# existing file instead would get the new binary killed on macOS, which
# caches the old file's code signature.
tmp="${target}.download.$$"
trap 'rm -f "$tmp"' EXIT
if command -v curl >/dev/null 2>&1; then
  curl -fsSL "$url" -o "$tmp"
elif command -v wget >/dev/null 2>&1; then
  wget -qO "$tmp" "$url"
else
  echo "Neither curl nor wget is available." >&2
  exit 1
fi

chmod +x "$tmp"
mv -f "$tmp" "$target"

echo ""
echo "kabby ${VERSION} installed to ${target}"
echo ""
echo "Make sure ${target} is on your PATH, then run:"
echo "  kabby --help"
