package main

import (
	"context"
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
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/forkid"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/p2p"
	"github.com/ethereum/go-ethereum/p2p/enode"
)

// Eth protocol message codes
const (
	StatusMsg                     = 0x00
	TransactionsMsg               = 0x02
	NewPooledTransactionHashesMsg = 0x08
	GetPooledTransactionsMsg      = 0x09
	PooledTransactionsMsg         = 0x0a
)

// statusData represents the EIP-2124 status packet for eth/68
// in the order: ProtocolVersion, NetworkID, TD, Head, Genesis, ForkID
// forkid.ID implements the RLP encoding for forkID data
// See: https://eips.ethereum.org/EIPS/eip-2124

type statusData struct {
	ProtocolVersion uint32
	NetworkID       uint64
	TD              *big.Int
	Head            common.Hash
	Genesis         common.Hash
	ForkID          forkid.ID
}

// TxInfo holds transaction details for JSON output

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

// TxMonitor stores state for incoming transactions and chain data client
type TxMonitor struct {
	rpcClient   *ethclient.Client
	txs         map[string]TxInfo
	knownHashes map[string]bool
	mutex       sync.RWMutex
	txLogFile   *os.File
}

var (
	httpAddr  = flag.String("http", ":8080", "HTTP API address")
	networkID = flag.Uint64("networkid", 1, "Network ID, e.g. 1 for mainnet")
	txLogPath = flag.String("txlog", "transactions.log", "Transaction log file path")
	bootnodes = []string{
		"enode://d860a01f9722d78051619d1e2351aba3f43f943f6f00718d1b9baa4101932a1f5011f16bb2b1bb35db20d6fe28fa0bf09636d26a87d31de9ec6203eeedb1f666@18.138.108.67:30303",
		"enode://22a8232c3abc76a16ae9d6c3b164f98775fe226f0917b0ca871128a74a8e9630b458460865bab457221f1d448dd9791d24c4e5d88786180ac185df813a68d4de@3.209.45.79:30303",
	}
)

func main() {
	flag.Parse()

	// Initialize RPC client
	rpc, err := ethclient.DialContext(context.Background(), "http://localhost:8545")
	if err != nil {
		log.Fatalf("RPC dial error: %v", err)
	}
	defer rpc.Close()

	// Open transaction log file
	txFile, err := os.OpenFile(*txLogPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatalf("Tx log file error: %v", err)
	}
	defer txFile.Close()

	// Parse static nodes
	nodeKey, _ := crypto.GenerateKey()
	var staticNodes []*enode.Node
	for _, uri := range bootnodes {
		if n, err := enode.ParseV4(uri); err == nil {
			staticNodes = append(staticNodes, n)
		}
	}

	// Setup monitor state
	monitor := &TxMonitor{
		rpcClient:   rpc,
		txs:         make(map[string]TxInfo),
		knownHashes: make(map[string]bool),
		txLogFile:   txFile,
	}

	// Configure P2P server
	srv := &p2p.Server{Config: p2p.Config{
		PrivateKey:  nodeKey,
		MaxPeers:    50,
		Name:        "eth-tx-monitor",
		ListenAddr:  "0.0.0.0:30303",
		NoDiscovery: false,
		StaticNodes: staticNodes,
		Protocols: []p2p.Protocol{{
			Name:    "eth",
			Version: 68,
			Length:  17,
			Run:     monitor.HandlePeer,
		}},
	}}

	// Start P2P
	if err := srv.Start(); err != nil {
		log.Fatalf("P2P start error: %v", err)
	}
	defer srv.Stop()

	// Subscribe to peer events
	events := make(chan *p2p.PeerEvent, 16)
	srv.SubscribeEvents(events)
	go func() {
		for ev := range events {
			log.Printf(
				"Peer event Type=%v Peer=%s Local=%s Remote=%s Error=%s MsgCode=%v MsgSize=%v Protocol=%s",
				ev.Type, ev.Peer, ev.LocalAddress, ev.RemoteAddress,
				ev.Error, ev.MsgCode, ev.MsgSize, ev.Protocol,
			)
		}
	}()

	// Log peer count periodically
	go func() {
		for range time.Tick(30 * time.Second) {
			log.Printf("Connected peers: %d", srv.PeerCount())
		}
	}()

	// HTTP API
	http.HandleFunc("/txs", monitor.handleTxs)
	log.Printf("HTTP listening on %s", *httpAddr)
	go func() {
		if err := http.ListenAndServe(*httpAddr, nil); err != nil {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	log.Println("P2P node listening on port 30303")
	select {}
}

// handleTxs serves stored transactions as JSON
func (tm *TxMonitor) handleTxs(w http.ResponseWriter, r *http.Request) {
	tm.mutex.RLock()
	defer tm.mutex.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	out := make([]TxInfo, 0, len(tm.txs))
	for _, tx := range tm.txs {
		out = append(out, tx)
	}
	json.NewEncoder(w).Encode(out)
}

// getHeadAndTD retrieves head hash, total difficulty, and genesis
func getHeadAndTD(rpc *ethclient.Client) (common.Hash, *big.Int, *types.Block, error) {
	ctx := context.Background()
	
	head, err := rpc.HeaderByNumber(ctx, nil)
	if err != nil {
		return common.Hash{}, nil, nil, err
	}
	genesis, err := rpc.BlockByNumber(ctx, big.NewInt(0))
	if err != nil {
		return common.Hash{}, nil, nil, err
	}
	// Raw RPC for totalDifficulty
	var res struct{ TotalDifficulty *hexutil.Big `json:"totalDifficulty"` }
	if err := rpc.Client().CallContext(ctx, &res, "eth_getBlockByNumber", "latest", false); err != nil {
		return common.Hash{}, nil, nil, err
	}
	return head.Hash(), (*big.Int)(res.TotalDifficulty), genesis, nil
}

// HandlePeer performs eth/68 handshake and processes messages
func (tm *TxMonitor) HandlePeer(peer *p2p.Peer, rw p2p.MsgReadWriter) error {
	// Fetch chain data
	headHash, headTD, genesisBlock, err := getHeadAndTD(tm.rpcClient)
	if err != nil {
		return fmt.Errorf("fetch head/TD: %w", err)
	}
	// Compute fork ID
	headHeader, err := tm.rpcClient.HeaderByNumber(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("fetch head header: %w", err)
	}
	fk := forkid.NewID(params.MainnetChainConfig, genesisBlock, headHeader.Number.Uint64(), headHeader.Time)

	// Send status
	status := &statusData{
		ProtocolVersion: 68,
		NetworkID:       *networkID,
		TD:              headTD,
		Head:            headHash,
		Genesis:         genesisBlock.Hash(),
		ForkID:          fk,
	}
	if err := p2p.Send(rw, StatusMsg, status); err != nil {
		return err
	}

	// Read peer status
	msg, err := rw.ReadMsg()
	if err != nil {
		return err
	}
	if msg.Code != StatusMsg {
		return fmt.Errorf("expected status msg code, got %d", msg.Code)
	}
	var peerStatus statusData
	if err := msg.Decode(&peerStatus); err != nil {
		return err
	}

	log.Printf("Connected to peer: %s", peer.ID())
	defer log.Printf("Disconnected from peer: %s", peer.ID())

	// Poll for tx hashes
	go func() {
		for range time.Tick(30 * time.Second) {
			p2p.Send(rw, NewPooledTransactionHashesMsg, []common.Hash{})
		}
	}()

	// Message loop
	for {
		msg, err := rw.ReadMsg()
		if err != nil {
			return err
		}
		switch msg.Code {
		case TransactionsMsg:
			var txs []*types.Transaction
			if msg.Decode(&txs) == nil {
				tm.processTxs(txs)
			}
		case NewPooledTransactionHashesMsg:
			var hashes []common.Hash
			if msg.Decode(&hashes) == nil {
				p2p.Send(rw, GetPooledTransactionsMsg, hashes)
			}
		case PooledTransactionsMsg:
			var txs []*types.Transaction
			if msg.Decode(&txs) == nil {
				tm.processTxs(txs)
			}
		}
	}
}

// processTxs logs and stores new transactions
func (tm *TxMonitor) processTxs(txs []*types.Transaction) {
	tm.mutex.Lock()
	defer tm.mutex.Unlock()
	for _, tx := range txs {
		h := tx.Hash().Hex()
		if tm.knownHashes[h] {
			continue
		}
		tm.knownHashes[h] = true
		// Extract sender
		signer := types.LatestSignerForChainID(tx.ChainId())
		from, _ := types.Sender(signer, tx)
		// Recipient
		var toAddr string
		if tx.To() != nil {
			toAddr = tx.To().Hex()
		}
		// Build record
		txInfo := TxInfo{
			Hash:      h,
			Value:     tx.Value().String(),
			From:      from.Hex(),
			To:        toAddr,
			Gas:       tx.Gas(),
			GasPrice:  tx.GasPrice().String(),
			Nonce:     tx.Nonce(),
			Timestamp: time.Now(),
		}
		// Store and log
		tm.txs[h] = txInfo
		b, _ := json.Marshal(txInfo)
		tm.txLogFile.Write(b)
		tm.txLogFile.WriteString("\n")
		// Trim to 1000
		if len(tm.txs) > 1000 {
			for k := range tm.txs {
				delete(tm.txs, k)
				break
			}
		}
	}
}
