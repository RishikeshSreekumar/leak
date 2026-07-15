#!/bin/sh
# Leak installer — downloads the correct prebuilt binary from the latest
# GitHub release, verifies its checksum, and installs it.
#
#   curl -fsSL https://raw.githubusercontent.com/RishikeshSreekumar/leak/main/install.sh | sh
#
# Environment overrides:
#   LEAK_INSTALL_DIR   destination directory (default: /usr/local/bin)
#   LEAK_VERSION       version to install, e.g. v0.1.0 (default: latest)
set -eu

REPO="RishikeshSreekumar/leak"
BINARY="leak"
INSTALL_DIR="${LEAK_INSTALL_DIR:-/usr/local/bin}"

info() { printf '%s\n' "$*"; }
err() { printf 'error: %s\n' "$*" >&2; exit 1; }

need() { command -v "$1" >/dev/null 2>&1 || err "required command not found: $1"; }
need uname
need tar

# Prefer curl, fall back to wget.
if command -v curl >/dev/null 2>&1; then
  DL="curl -fsSL"
  DLO="curl -fsSL -o"
elif command -v wget >/dev/null 2>&1; then
  DL="wget -qO-"
  DLO="wget -qO"
else
  err "need curl or wget"
fi

# Detect OS.
os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
  linux) os=linux ;;
  darwin) os=darwin ;;
  *) err "unsupported OS: $os (use a prebuilt binary from https://github.com/$REPO/releases)" ;;
esac

# Detect arch.
arch=$(uname -m)
case "$arch" in
  x86_64 | amd64) arch=amd64 ;;
  aarch64 | arm64) arch=arm64 ;;
  *) err "unsupported architecture: $arch" ;;
esac

# Resolve version.
version="${LEAK_VERSION:-}"
if [ -z "$version" ]; then
  info "Resolving latest release..."
  version=$($DL "https://api.github.com/repos/$REPO/releases/latest" \
    | grep -m1 '"tag_name"' \
    | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')
  [ -n "$version" ] || err "could not resolve latest version"
fi

# GoReleaser strips the leading v from {{ .Version }} in archive names.
ver_noprefix=$(printf '%s' "$version" | sed 's/^v//')
archive="${BINARY}_${ver_noprefix}_${os}_${arch}.tar.gz"
base="https://github.com/$REPO/releases/download/$version"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

info "Downloading $archive ($version)..."
$DLO "$tmp/$archive" "$base/$archive" || err "download failed: $base/$archive"

# Verify checksum when a sha256 tool is available.
if command -v sha256sum >/dev/null 2>&1; then
  SHA="sha256sum"
elif command -v shasum >/dev/null 2>&1; then
  SHA="shasum -a 256"
else
  SHA=""
fi
if [ -n "$SHA" ]; then
  info "Verifying checksum..."
  $DLO "$tmp/checksums.txt" "$base/checksums.txt" || err "could not download checksums.txt"
  want=$(grep " $archive\$" "$tmp/checksums.txt" | awk '{print $1}')
  [ -n "$want" ] || err "checksum for $archive not found"
  got=$($SHA "$tmp/$archive" | awk '{print $1}')
  [ "$want" = "$got" ] || err "checksum mismatch (want $want, got $got)"
else
  info "warning: no sha256 tool found; skipping checksum verification"
fi

info "Extracting..."
tar -xzf "$tmp/$archive" -C "$tmp"
[ -f "$tmp/$BINARY" ] || err "binary not found in archive"
chmod +x "$tmp/$BINARY"

# Install, escalating with sudo only if the directory isn't writable.
mkdir -p "$INSTALL_DIR" 2>/dev/null || true
if [ -w "$INSTALL_DIR" ]; then
  mv "$tmp/$BINARY" "$INSTALL_DIR/$BINARY"
elif command -v sudo >/dev/null 2>&1; then
  info "Installing to $INSTALL_DIR (needs sudo)..."
  sudo mv "$tmp/$BINARY" "$INSTALL_DIR/$BINARY"
else
  err "$INSTALL_DIR is not writable and sudo is unavailable; set LEAK_INSTALL_DIR to a writable path"
fi

info "Installed $BINARY $version to $INSTALL_DIR/$BINARY"
case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *) info "note: $INSTALL_DIR is not on your PATH — add it to use 'leak' directly." ;;
esac
info "Run 'leak --version' to confirm."
