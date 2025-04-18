#!/usr/bin/env bash
set -euo pipefail

REPO="erigontech/erigon"
API_URL="https://api.github.com/repos/${REPO}/releases/latest"
GO_VERSION="1.23.6"
GO_TARBALL="go${GO_VERSION}.linux-amd64.tar.gz"
GO_URL="https://go.dev/dl/${GO_TARBALL}"
INSTALL_DIR="/usr/local"
CLONE_DIR="erigon"

# 1. Ensure Go ≥ 1.23.x
if ! command -v go >/dev/null || ! go version | grep -q "go1\.23"; then
  echo "Installing Go ${GO_VERSION}..."
  curl -fsSL "${GO_URL}" -o "/tmp/${GO_TARBALL}"
  sudo rm -rf "${INSTALL_DIR}/go"
  sudo tar -C "${INSTALL_DIR}" -xzf "/tmp/${GO_TARBALL}"
  export PATH="${INSTALL_DIR}/go/bin:${PATH}"
  echo "Go installed: $(go version)"
else
  echo "Go OK: $(go version)"
fi

# 2. Fetch latest Erigon release tag
echo "Fetching latest Erigon release..."
TAG=$(curl -fsSL "${API_URL}" \
      | grep -Po '"tag_name": "\K.*?(?=")')
echo "Latest release: ${TAG}"
VERSION="${TAG#v}"        # strip leading "v"
BRANCH="release/${VERSION}"

# 3. Clone or update repo
if [ -d "${CLONE_DIR}" ]; then
  echo "Updating existing ${CLONE_DIR}/…"
  git -C "${CLONE_DIR}" pull --ff-only \
    && echo "✅ Pulled latest changes." \
    || echo "⚠️  Pull failed; you may need to resolve conflicts or remove ${CLONE_DIR}/ and retry."
else
  echo "Cloning ${REPO} (${BRANCH})…"
  git clone --branch "${BRANCH}" --single-branch --depth 1 \
        "https://github.com/${REPO}.git" "${CLONE_DIR}" \
    || git clone --branch "${TAG}" --single-branch --depth 1 \
        "https://github.com/${REPO}.git" "${CLONE_DIR}"
fi

cd "${CLONE_DIR}"

# 4. Build Erigon
echo "Building Erigon…"
make erigon

# 5. Run in minimal prune mode
echo "Starting Erigon in minimal mode…"
exec ./build/bin/erigon --prune.mode=minimal
