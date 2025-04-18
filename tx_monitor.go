package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/p2p"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/p2p/nat"
	"github.com/ethereum/go-ethereum/rlp"
)

const (
	StatusMsg                     = 0x00
	TransactionsMsg               = 0x02
	NewPooledTransactionHashesMsg = 0x08
	GetPooledTransactionsMsg      = 0x09
	PooledTransactionsMsg         = 0x0a
)

type ForkID struct {
	Hash [4]byte
	Next uint64
}

type StatusPacket struct {
	ProtocolVersion uint32
	NetworkID       uint64
	TD              *big.Int
	Head            common.Hash
	Genesis         common.Hash
	ForkID          ForkID
}

type TxInfo struct {
	Hash      string    `json:"hash"`
	Value     string    `json:"value"`
	From      string    `json:"from,omitempty"`
	To        string    `json:"to,omitempty"`
	Gas       uint64    `json:"gas"`
	GasPrice  string    `json:"gasPrice"`
	Nonce     uint64    `json:"nonce"`
	Timestamp time.Time `json:"timestamp"`
}

type TxMonitor struct {
	txs         map[string]TxInfo
	knownHashes map[string]bool
	mutex       sync.RWMutex
	nodeID      string
	txLogFile   *os.File
}

var (
	listenAddr = flag.String("addr", ":30303", "P2P listen address")
	httpAddr   = flag.String("http", ":8080", "HTTP API address")
	networkID  = flag.Uint64("networkid", 1, "Network ID")
	txLogFile  = flag.String("txlog", "transactions.log", "Transaction log file")

	defaultBootnodes = []string{
		"enode://d860a01f9722d78051619d1e2351aba3f43f943f6f00718d1b9baa4101932a1f5011f16bb2b1bb35db20d6fe28fa0bf09636d26a87d31de9ec6203eeedb1f666@18.138.108.67:30303",
		"enode://22a8232c3abc76a16ae9d6c3b164f98775fe226f0917b0ca871128a74a8e9630b458460865bab457221f1d448dd9791d24c4e5d88786180ac185df813a68d4de@3.209.45.79:30303",
	}
)

func main() {
	flag.Parse()

	// Open log file
	txFile, err := os.OpenFile(*txLogFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatalf("Failed to open transaction log file: %v", err)
	}
	defer txFile.Close()
	log.Printf("Saving transactions to %s", *txLogFile)

	// Generate node key and parse bootnodes
	nodeKey, _ := crypto.GenerateKey()
	nodeID := enode.PubkeyToIDV4(&nodeKey.PublicKey)

	var bootnodes []*enode.Node
	for _, url := range defaultBootnodes {
		if n, err := enode.ParseV4(url); err == nil {
			bootnodes = append(bootnodes, n)
		}
	}

	// Create monitor
	monitor := &TxMonitor{
		txs:         make(map[string]TxInfo),
		knownHashes: make(map[string]bool),
		nodeID:      nodeID.String(),
		txLogFile:   txFile,
	}

	// Configure P2P server
	srv := &p2p.Server{
		Config: p2p.Config{
			PrivateKey:     nodeKey,
			MaxPeers:       50,
			Name:           "ETH TxMonitor",
			ListenAddr:     *listenAddr,
			NAT:            nat.Any(),
			BootstrapNodes: bootnodes,
			Protocols: []p2p.Protocol{{
				Name:    "eth",
				Version: 68,
				Length:  17,
				Run:     monitor.HandlePeer,
			}},
		},
	}

	// Start server
	if err := srv.Start(); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
	defer srv.Stop()

	// Start HTTP API
	http.HandleFunc("/txs", monitor.handleTxs)
	http.HandleFunc("/", monitor.handleTxs)
	go http.ListenAndServe(*httpAddr, nil)

	log.Printf("P2P server started on %s, HTTP on %s", *listenAddr, *httpAddr)

	// Keep application alive
	select {}
}

func (tm *TxMonitor) handleTxs(w http.ResponseWriter, r *http.Request) {
	tm.mutex.RLock()
	defer tm.mutex.RUnlock()

	w.Header().Set("Content-Type", "application/json")

	txs := make([]TxInfo, 0, len(tm.txs))
	for _, tx := range tm.txs {
		txs = append(txs, tx)
	}

	json.NewEncoder(w).Encode(txs)
}

func (tm *TxMonitor) HandlePeer(peer *p2p.Peer, rw p2p.MsgReadWriter) error {
	log.Printf("Connected to peer: %s", peer.ID().String())
	defer log.Printf("Disconnected from peer: %s", peer.ID().String())

	// Ethereum mainnet genesis hash
	genesisHash := common.HexToHash("0xd4e56740f876aef8c010b86a40d5f56745a118d0906a34e69aec8c0db1cb8fa3")

	// Do handshake
	status := &StatusPacket{
		ProtocolVersion: 68,
		NetworkID:       *networkID,
		TD:              big.NewInt(1),
		Head:            common.HexToHash("0x2386d1e9dea52b3496e1edccd13515f9b43689da9e8f9a61d6737a6000000000"),
		Genesis:         genesisHash,
		ForkID:          ForkID{Hash: [4]byte{0x40, 0xd2, 0xdc, 0x28}, Next: 0},
	}

	if err := p2p.Send(rw, StatusMsg, status); err != nil {
		return err
	}

	// Read peer status
	msg, err := rw.ReadMsg()
	if err != nil || msg.Code != StatusMsg {
		return fmt.Errorf("invalid status message: %v", err)
	}

	var peerStatus StatusPacket
	if err := msg.Decode(&peerStatus); err != nil {
		return fmt.Errorf("failed to decode status: %v", err)
	}

	// Periodically ask for transactions
	go func() {
		for {
			if err := p2p.Send(rw, TransactionsMsg, []*types.Transaction{}); err != nil {
				return
			}
			time.Sleep(30 * time.Second)
		}
	}()

	// Main message loop
	for {
		msg, err := rw.ReadMsg()
		if err != nil {
			return err
		}

		switch msg.Code {
		case TransactionsMsg:
			var txs []*types.Transaction
			if err := msg.Decode(&txs); err == nil && len(txs) > 0 {
				log.Printf("Received %d transactions", len(txs))
				tm.processTxs(txs)
			}

		case NewPooledTransactionHashesMsg:
			var hashes []common.Hash
			if err := msg.Decode(&hashes); err != nil {
				// Try alternate format
				var hashesBytes []byte
				if err := msg.Decode(&hashesBytes); err == nil {
					rlp.DecodeBytes(hashesBytes, &hashes)
				}
			}

			if len(hashes) > 0 {
				log.Printf("Received %d tx hashes, requesting txs", len(hashes))
				p2p.Send(rw, GetPooledTransactionsMsg, hashes)
			}

		case PooledTransactionsMsg:
			var txs []*types.Transaction
			if err := msg.Decode(&txs); err == nil && len(txs) > 0 {
				log.Printf("Received %d pooled transactions", len(txs))
				tm.processTxs(txs)
			}
		}
	}
}

func (tm *TxMonitor) processTxs(txs []*types.Transaction) {
	tm.mutex.Lock()
	defer tm.mutex.Unlock()

	for _, tx := range txs {
		hash := tx.Hash().Hex()

		if tm.knownHashes[hash] {
			continue
		}

		tm.knownHashes[hash] = true

		signer := types.LatestSignerForChainID(tx.ChainId())
		from, _ := types.Sender(signer, tx)

		var to string
		if tx.To() != nil {
			to = tx.To().Hex()
		}

		txInfo := TxInfo{
			Hash:      hash,
			Value:     tx.Value().String(),
			From:      from.Hex(),
			To:        to,
			Gas:       tx.Gas(),
			GasPrice:  tx.GasPrice().String(),
			Nonce:     tx.Nonce(),
			Timestamp: time.Now(),
		}

		// Store transaction
		tm.txs[hash] = txInfo

		// Save to log file
		if txJson, err := json.Marshal(txInfo); err == nil {
			tm.txLogFile.WriteString(string(txJson) + "\n")
		}

		// Limit storage to 1000 transactions
		if len(tm.txs) > 1000 {
			for k := range tm.txs {
				delete(tm.txs, k)
				break
			}
		}
	}
}
