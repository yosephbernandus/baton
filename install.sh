#!/bin/sh
# Baton installer — works with bash, zsh, fish (via sh), and POSIX sh.
# Usage: curl -fsSL https://raw.githubusercontent.com/yosephbernandus/baton/main/install.sh | sh

set -e

REPO="yosephbernandus/baton"
INSTALL_DIR="${BATON_INSTALL_DIR:-/usr/local/bin}"

# Detect OS
OS="$(uname -s)"
case "$OS" in
    Linux)  OS="linux" ;;
    Darwin) OS="darwin" ;;
    *)      echo "Unsupported OS: $OS"; exit 1 ;;
esac

# Detect architecture
ARCH="$(uname -m)"
case "$ARCH" in
    x86_64|amd64)   ARCH="amd64" ;;
    aarch64|arm64)   ARCH="arm64" ;;
    *)               echo "Unsupported architecture: $ARCH"; exit 1 ;;
esac

# Get latest version
echo "Detecting latest version..."
VERSION="$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | head -1 | sed 's/.*"tag_name": *"//;s/".*//')"

if [ -z "$VERSION" ]; then
    echo "Error: could not detect latest version."
    echo "Check https://github.com/${REPO}/releases"
    exit 1
fi

echo "Installing baton ${VERSION} (${OS}/${ARCH})..."

# Build download URL
FILENAME="baton_${VERSION#v}_${OS}_${ARCH}.tar.gz"
URL="https://github.com/${REPO}/releases/download/${VERSION}/${FILENAME}"

# Download and extract
TMPDIR="$(mktemp -d)"
trap 'rm -rf "$TMPDIR"' EXIT

echo "Downloading ${URL}..."
curl -fsSL "$URL" -o "${TMPDIR}/baton.tar.gz"

tar -xzf "${TMPDIR}/baton.tar.gz" -C "$TMPDIR"

# Install binary
if [ -w "$INSTALL_DIR" ]; then
    mv "${TMPDIR}/baton" "${INSTALL_DIR}/baton"
else
    echo "Need sudo to install to ${INSTALL_DIR}"
    sudo mv "${TMPDIR}/baton" "${INSTALL_DIR}/baton"
fi

chmod +x "${INSTALL_DIR}/baton"

echo ""
echo "baton ${VERSION} installed to ${INSTALL_DIR}/baton"
echo ""

# Verify
if command -v baton >/dev/null 2>&1; then
    echo "Verify: $(baton --version)"
else
    echo "Note: ${INSTALL_DIR} may not be in your PATH."
    echo "Add it:"
    echo "  bash/zsh: export PATH=\"${INSTALL_DIR}:\$PATH\""
    echo "  fish:     set -gx PATH ${INSTALL_DIR} \$PATH"
fi
