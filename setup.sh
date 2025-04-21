#!/usr/bin/env bash
set -euo pipefail

apt-get update
apt-get install -y curl jq libsnappy-dev libc6-dev unzip build-essential git

REPO="erigontech/erigon"
API_URL="https://api.github.com/repos/${REPO}/releases/latest"
GO_VERSION="1.23.6"
GO_TARBALL="go${GO_VERSION}.linux-amd64.tar.gz"
GO_URL="https://go.dev/dl/${GO_TARBALL}"
INSTALL_DIR="/usr/local"

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

# 3. Clone that release
VERSION="${TAG#v}"           # strip leading "v"
BRANCH="release/${VERSION}"
echo "Cloning ${REPO} (${BRANCH})..."
git clone --branch "${BRANCH}" --single-branch --depth 1 \
      "https://github.com/${REPO}.git" erigon \
  || git clone --branch "${TAG}" --single-branch --depth 1 \
      "https://github.com/${REPO}.git" erigon

cd erigon

# 4. Build
echo "Building Erigon..."
make erigon
