package main

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/btcsuite/btcd/blockchain"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/fountain/bootstrap"
	"github.com/btcsuite/btcd/fountain/distribution"
	"github.com/btcsuite/btcd/fountain/droplet"
	"github.com/btcsuite/btcd/wire"
	"github.com/btcsuite/btclog"
)

type retryHeaderSource struct {
	headers map[int32]wire.BlockHeader
}

func (s *retryHeaderSource) BlockHashByHeight(height int32) (*chainhash.Hash, error) {
	header, ok := s.headers[height]
	if !ok {
		return nil, fmt.Errorf("no block at height %d exists", height)
	}
	hash := header.BlockHash()
	return &hash, nil
}

func (s *retryHeaderSource) HeaderByHash(hash *chainhash.Hash) (wire.BlockHeader, error) {
	for _, header := range s.headers {
		if header.BlockHash() == *hash {
			return header, nil
		}
	}
	return wire.BlockHeader{}, fmt.Errorf("unknown header %s", hash)
}

func retryTestBlocks(t *testing.T, count int) ([][]byte, map[int32]wire.BlockHeader) {
	t.Helper()

	blocks := make([][]byte, count)
	headers := make(map[int32]wire.BlockHeader, count)
	for i := range count {
		msg := wire.NewMsgBlock(&wire.BlockHeader{Nonce: uint32(i)})
		tx := wire.NewMsgTx(1)
		tx.AddTxIn(wire.NewTxIn(
			&wire.OutPoint{Index: wire.MaxPrevOutIndex}, []byte{byte(i)}, nil,
		))
		tx.AddTxOut(wire.NewTxOut(int64(i+1), []byte{0x51}))
		if err := msg.AddTransaction(tx); err != nil {
			t.Fatalf("AddTransaction(%d): %v", i, err)
		}

		merkles := blockchain.BuildMerkleTreeStore(
			btcutil.NewBlock(msg).Transactions(), false,
		)
		msg.Header.MerkleRoot = *merkles[len(merkles)-1]

		var serialized bytes.Buffer
		if err := msg.Serialize(&serialized); err != nil {
			t.Fatalf("Serialize(%d): %v", i, err)
		}
		blocks[i] = serialized.Bytes()
		headers[int32(i)] = msg.Header
	}
	return blocks, headers
}

func TestRetryPendingFountainDecodeAfterHeadersArrive(t *testing.T) {
	srvrLog.SetLevel(btclog.LevelOff)
	defer srvrLog.SetLevel(btclog.LevelInfo)

	const k = 3
	blocks, headers := retryTestBlocks(t, k)
	seed := [32]byte{1}
	params := droplet.NewEpochParams(0, k, seed)
	encoder := droplet.NewEncoder(
		&params, distribution.NewRobustSoliton(k, 0.3, 0.05), blocks,
	)
	droplets := encoder.GenerateN(30)

	collector := bootstrap.NewCollector(bootstrap.Config{K: k, C: 0.3, Delta: 0.05})
	info := wire.FtnEpochInfo{EpochID: 0, EpochSeed: seed, NumDroplets: uint64(len(droplets))}
	if err := collector.RegisterEpoch(1, info); err != nil {
		t.Fatalf("RegisterEpoch: %v", err)
	}
	records := make([]*wire.DropletRecord, 0, len(droplets))
	for _, d := range droplets {
		records = append(records, &wire.DropletRecord{
			DropletID: d.DropletID,
			PaddedLen: d.PaddedLen,
			Payload:   append([]byte(nil), d.Payload...),
		})
	}
	if _, err := collector.AddDroplets(0, 1, records); err != nil {
		t.Fatalf("AddDroplets: %v", err)
	}

	source := &retryHeaderSource{headers: make(map[int32]wire.BlockHeader)}
	s := &server{fountainBootstrap: collector, fountainHeaders: source}
	s.retryPendingFountainDecodes()
	if collector.Decoded(0) {
		t.Fatal("epoch decoded before headers were available")
	}

	source.headers = headers
	s.retryPendingFountainDecodes()
	if !collector.Decoded(0) {
		t.Fatal("epoch was not decoded after headers became available")
	}
}
