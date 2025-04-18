# Ethereum Transaction Monitor

A tool for monitoring Ethereum network transactions in real-time.

## Prerequisites

- Go 1.23+
- Ethereum go-ethereum dependencies (automatically installed via go modules)

## Getting Started

1. **Run the transaction monitor**
   ```
   ./run_tx_monitor.sh
   ```

   Optional flags:
   - `-addr :30303` - P2P network listen address and port
   - `-nat none` - NAT port mapping mechanism (any|none|upnp|pmp|extip:<IP>)
   - `-nodekey filename` - Private key filename
   - `-verbosity 3` - Log verbosity (0-5)
   - `-bootnodes comma,separated,nodes` - Custom bootstrap nodes
   - `-http :8080` - HTTP server address for viewing transactions

2. **View transactions**
   
   Open your browser and navigate to:
   ```
   http://localhost:8080/txs
   ```

## Troubleshooting

If you're not seeing any transactions:

1. Check your firewall settings to ensure port 30303 (or your custom port) is open
2. Verify your internet connection
3. It may take time to connect to peers on the Ethereum network
4. Check logs for any error messages

## How It Works

The transaction monitor connects to the Ethereum P2P network and listens for transaction announcements from connected peers. It stores these transactions in memory and provides a simple HTTP API for viewing them.

Transactions are available at the `/txs` endpoint as JSON data. 