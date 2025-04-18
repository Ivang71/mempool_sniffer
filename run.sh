#!/usr/bin/env bash
set -euo pipefail

sudo systemctl stop erigon.service              || true
sudo systemctl disable erigon.service           || true
sudo rm /etc/systemd/system/erigon.service      || true
sudo rm /etc/systemd/system/multi-user.target.wants/erigon.service || true
sudo rm /lib/systemd/system/erigon.service      || true
sudo systemctl daemon-reload                    || true
sudo systemctl reset-failed                     || true
sudo rm /usr/local/bin/erigon                   || true
sudo rm -rf /var/lib/erigon                     || true
sudo userdel erigon                             || true


# 1) Install dependencies
#   curl, jq, snappy libs, and unzip for binary extraction
apt-get update
apt-get install -y curl jq libsnappy-dev libc6-dev unzip build-essential git

# 2) Install Go (if not already installed)
if ! command -v go &>/dev/null; then
  GO_VER=1.20.10
  wget https://golang.org/dl/go${GO_VER}.linux-amd64.tar.gz
  tar -C /usr/local -xzf go${GO_VER}.linux-amd64.tar.gz
  ln -s /usr/local/go/bin/go /usr/local/bin/go
  echo 'export PATH=$PATH:/usr/local/go/bin' >> /etc/profile.d/go.sh
  source /etc/profile.d/go.sh
  rm -f go${GO_VER}.linux-amd64.tar.gz
fi

# 3) Download and install Erigon binary
ERIGON_VER=v3.0.1
wget https://github.com/erigontech/erigon/releases/download/${ERIGON_VER}/erigon_${ERIGON_VER}_linux_amd64.tar.gz \
  -O erigon_${ERIGON_VER}_linux_amd64.tar.gz
mkdir -p /tmp/erigon_extract
tar -xzf erigon_${ERIGON_VER}_linux_amd64.tar.gz -C /tmp/erigon_extract
# Make sure we're copying the correct binary from the extracted directory
cp /tmp/erigon_extract/erigon_${ERIGON_VER}_linux_amd64/erigon /usr/local/bin/
chmod +x /usr/local/bin/erigon
rm -rf /tmp/erigon_extract

# 4) Create system user and data directories
useradd --system --no-create-home --shell /usr/sbin/nologin erigon || true
mkdir -p /var/lib/erigon
chown erigon:erigon /var/lib/erigon

# 5) Create Systemd service for Erigon
cat > /etc/systemd/system/erigon.service << 'EOF'
[Unit]
Description=Erigon Minimal Node
After=network.target

[Service]
User=erigon
Group=erigon
ExecStart=/usr/local/bin/erigon \
  --chain mainnet \
  --datadir /var/lib/erigon \
  --prune.mode minimal \
  --http \
    --http.addr 0.0.0.0 \
    --http.port 8545 \
    --http.api eth,net,web3,txpool,engine \
  --ws \
    --ws.port 8546
Restart=always
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
EOF

# 6) Enable and start Erigon service
systemctl daemon-reload
systemctl enable erigon
systemctl restart erigon

# 7) Clean up downloaded files
rm -f erigon_${ERIGON_VER}_linux_amd64.tar.gz
