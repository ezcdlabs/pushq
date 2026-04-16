#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DEST="$(which git-pushq 2>/dev/null || echo "/usr/local/bin/git-pushq")"
BIN="$REPO_ROOT/git-pushq"

DEV_VERSION="dev-$(date +%s)"
echo "Building git-pushq ($DEV_VERSION)..."
go build -ldflags "-X main.version=$DEV_VERSION" -o "$BIN" "$REPO_ROOT/cmd/git-pushq"

echo "Installing to $DEST..."
if [ -w "$(dirname "$DEST")" ]; then
    mv "$BIN" "$DEST"
else
    sudo mv "$BIN" "$DEST"
fi

echo "Installed: $(git-pushq --version)"
