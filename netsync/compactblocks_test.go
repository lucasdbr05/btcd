// Copyright (c) 2026 The btcsuite developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package netsync

import (
	"bytes"
	"testing"

	"github.com/btcsuite/btcd/blockchain"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/wire"
	"github.com/stretchr/testify/require"
)

func makeCompactBlockTestBlock(numTxns int) *btcutil.Block {
	msgBlock := &wire.MsgBlock{
		Header: wire.BlockHeader{
			Version: 1,
			Bits:    0x1d00ffff,
			Nonce:   1,
		},
		Transactions: make([]*wire.MsgTx, numTxns),
	}

	txns := make([]*btcutil.Tx, numTxns)
	for i := 0; i < numTxns; i++ {
		msgTx := makeCompactBlockTestTx(byte(i))
		msgBlock.Transactions[i] = msgTx
		txns[i] = btcutil.NewTx(msgTx)
	}

	msgBlock.Header.MerkleRoot = blockchain.CalcMerkleRoot(txns, false)
	return btcutil.NewBlock(msgBlock)
}

func makeCompactBlockTestTx(tag byte) *wire.MsgTx {
	prevHash := chainhash.Hash{tag}
	msgTx := wire.NewMsgTx(1)
	msgTx.AddTxIn(&wire.TxIn{
		PreviousOutPoint: wire.OutPoint{
			Hash:  prevHash,
			Index: uint32(tag),
		},
		SignatureScript: []byte{tag},
		Sequence:        0xffffffff,
	})
	msgTx.AddTxOut(&wire.TxOut{
		Value:    int64(tag) + 1,
		PkScript: []byte{0x51, tag},
	})

	return msgTx
}

func makeCompactBlockTestBlockWithWitness(numTxns int) *btcutil.Block {
	msgBlock := &wire.MsgBlock{
		Header: wire.BlockHeader{
			Version: 1,
			Bits:    0x1d00ffff,
			Nonce:   2,
		},
		Transactions: make([]*wire.MsgTx, numTxns),
	}

	txns := make([]*btcutil.Tx, numTxns)
	for i := 0; i < numTxns; i++ {
		var msgTx *wire.MsgTx
		if i == 0 {
			// Coinbase — no witness.
			msgTx = makeCompactBlockTestTx(byte(i))
		} else {
			msgTx = makeCompactBlockTestTxWithWitness(byte(i))
		}
		msgBlock.Transactions[i] = msgTx
		txns[i] = btcutil.NewTx(msgTx)
	}

	msgBlock.Header.MerkleRoot = blockchain.CalcMerkleRoot(txns, false)
	return btcutil.NewBlock(msgBlock)
}

func makeCompactBlockTestTxWithWitness(tag byte) *wire.MsgTx {
	prevHash := chainhash.Hash{tag}
	msgTx := wire.NewMsgTx(1)
	msgTx.AddTxIn(&wire.TxIn{
		PreviousOutPoint: wire.OutPoint{
			Hash:  prevHash,
			Index: uint32(tag),
		},
		SignatureScript: nil,
		Sequence:        0xffffffff,
		Witness:         wire.TxWitness{[]byte{tag, tag + 1}},
	})
	msgTx.AddTxOut(&wire.TxOut{
		Value:    int64(tag) + 1,
		PkScript: []byte{0x51, tag},
	})

	return msgTx
}

// TestBuildCompactBlockWireRoundTrip serializes a cmpctblock message using the
// Bitcoin P2P wire encoding and deserializes it again, verifying that the
// header, nonce, short IDs, and prefilled transactions survive the round trip.
func TestBuildCompactBlockWireRoundTrip(t *testing.T) {
	block := makeCompactBlockTestBlock(4)
	const nonce = uint64(0xdeadbeef)

	original, err := BuildCompactBlock(block, nonce, []uint32{0}, false)
	require.NoError(t, err)

	// Encode
	var buf bytes.Buffer
	err = original.BtcEncode(&buf, wire.ShortIdsBlocksVersion,
		wire.BaseEncoding)
	require.NoError(t, err)

	// Decode
	decoded := &wire.MsgCmpctBlock{}
	err = decoded.BtcDecode(&buf, wire.ShortIdsBlocksVersion,
		wire.BaseEncoding)
	require.NoError(t, err)

	// Header must match
	origHash := original.Header.BlockHash()
	decHash := decoded.Header.BlockHash()
	require.Equal(t, origHash, decHash)

	// Nonce must match
	require.Equal(t, original.Nonce, decoded.Nonce)

	// Short ID count and values must match
	require.Equal(t, original.ShortIDs, decoded.ShortIDs)

	// Prefilled transaction count and indexes must match
	require.Len(t, decoded.PrefilledTxns, len(original.PrefilledTxns))
	for i := range original.PrefilledTxns {
		require.Equal(t, original.PrefilledTxns[i].Index, decoded.PrefilledTxns[i].Index)
		require.Equal(t, original.PrefilledTxns[i].Tx.TxHash(), decoded.PrefilledTxns[i].Tx.TxHash())
	}
}

func TestBuildCompactBlock(t *testing.T) {
	block := makeCompactBlockTestBlock(3)

	msg, err := BuildCompactBlock(block, 99, []uint32{2, 0}, false)
	require.NoError(t, err)

	require.Len(t, msg.ShortIDs, 1)
	require.Len(t, msg.PrefilledTxns, 2)

	gotIndexes := []uint32{
		msg.PrefilledTxns[0].Index,
		msg.PrefilledTxns[1].Index,
	}
	require.Equal(t, []uint32{0, 2}, gotIndexes)
}

func TestCompactBlockReconstructorCompleteFromMempool(t *testing.T) {
	block := makeCompactBlockTestBlock(3)
	msg, err := BuildCompactBlock(block, 99, nil, false)
	require.NoError(t, err)

	recon, err := NewCompactBlockReconstructor(
		msg, block.Transactions()[1:], false,
	)
	require.NoError(t, err)

	require.True(t, recon.IsComplete(), 
		"reconstructor should be complete, missing %v", recon.MissingIndexes())

	reconstructed, err := recon.Block()
	require.NoError(t, err)
	require.Equal(t, block.Hash(), reconstructed.Hash())
}

func TestCompactBlockReconstructorWithBlockTxn(t *testing.T) {
	block := makeCompactBlockTestBlock(3)
	msg, err := BuildCompactBlock(block, 99, nil, false)
	require.NoError(t, err)

	recon, err := NewCompactBlockReconstructor(
		msg, block.Transactions()[1:2], false,
	)
	require.NoError(t, err)

	wantMissing := []uint32{2}
	require.Equal(t, wantMissing, recon.MissingIndexes())

	req := recon.GetBlockTxnRequest()
	require.Equal(t, wantMissing, req.Indexes)

	resp := wire.NewMsgBlockTxn(&req.BlockHash)
	err = resp.AddTransaction(block.MsgBlock().Transactions[2])
	require.NoError(t, err)

	reconstructed, err := recon.ProvideBlockTxn(resp)
	require.NoError(t, err)
	require.Equal(t, block.Hash(), reconstructed.Hash())
}

func TestCompactBlockReconstructorBadMerkleRoot(t *testing.T) {
	block := makeCompactBlockTestBlock(3)
	msg, err := BuildCompactBlock(block, 99, nil, false)
	require.NoError(t, err)

	recon, err := NewCompactBlockReconstructor(
		msg, block.Transactions()[1:2], false,
	)
	require.NoError(t, err)

	req := recon.GetBlockTxnRequest()
	resp := wire.NewMsgBlockTxn(&req.BlockHash)
	resp.Transactions = []*wire.MsgTx{makeCompactBlockTestTx(42)}

	_, err = recon.ProvideBlockTxn(resp)
	require.ErrorIs(t, err, ErrCompactBlockBadMerkleRoot)
}

func TestShortIDNonceDependence(t *testing.T) {
	block := makeCompactBlockTestBlock(3)

	msg1, err := BuildCompactBlock(block, 1, nil, false)
	require.NoError(t, err)

	msg2, err := BuildCompactBlock(block, 2, nil, false)
	require.NoError(t, err)

	// Same nonce must produce identical short IDs.
	msg1b, err := BuildCompactBlock(block, 1, nil, false)
	require.NoError(t, err)
	require.Equal(t, msg1.ShortIDs, msg1b.ShortIDs)

	// Different nonces must produce different short IDs.
	require.NotEqual(t, msg1.ShortIDs, msg2.ShortIDs)
}

func TestShortIDSize(t *testing.T) {
	block := makeCompactBlockTestBlock(5)
	msg, err := BuildCompactBlock(block, 42, nil, false)
	require.NoError(t, err)

	for i, id := range msg.ShortIDs {
		require.Len(t, id, wire.ShortIDSize)
		// Verify the same ID is returned by CompactBlockShortIDFromHash.
		tx := block.Transactions()[i+1] // skip coinbase
		got, err := CompactBlockShortID(&block.MsgBlock().Header, 
			42, tx, false)
		require.NoError(t, err)
		require.Equal(t, id, got)
	}
}

// TestBuildCompactBlockAllPrefilled verifies that when every transaction is
// marked as prefilled the resulting message has no short IDs.
func TestBuildCompactBlockAllPrefilled(t *testing.T) {
	const numTxns = 4
	block := makeCompactBlockTestBlock(numTxns)

	allIndexes := make([]uint32, numTxns)
	for i := range allIndexes {
		allIndexes[i] = uint32(i)
	}

	msg, err := BuildCompactBlock(block, 0, allIndexes, false)
	require.NoError(t, err)

	require.Empty(t, msg.ShortIDs)
	require.Len(t, msg.PrefilledTxns, numTxns)

	// The block must be reconstructable from the prefilled txns alone
	// (empty mempool).
	recon, err := NewCompactBlockReconstructor(msg, nil, false)
	require.NoError(t, err)
	require.True(t, recon.IsComplete(), 
		"reconstructor should be complete, missing %v", recon.MissingIndexes())
	reconstructed, err := recon.Block()
	require.NoError(t, err)
	require.Equal(t, block.Hash(), reconstructed.Hash())
}

// TestCompactBlockReconstructorEmptyMempool verifies that when no mempool
// transactions are provided every non-prefilled transaction appears in
// MissingIndexes and they can be supplied via a blocktxn response.
func TestCompactBlockReconstructorEmptyMempool(t *testing.T) {
	block := makeCompactBlockTestBlock(4)
	msg, err := BuildCompactBlock(block, 7, nil, false)
	require.NoError(t, err)

	// No mempool — all non-prefilled txns should be missing.
	recon, err := NewCompactBlockReconstructor(msg, nil, false)
	require.NoError(t, err)
	require.False(t, recon.IsComplete(), 
		"reconstructor should not be complete with empty mempool")

	// Indexes 1, 2, 3 should be missing (0 is the prefilled coinbase).
	wantMissing := []uint32{1, 2, 3}
	require.Equal(t, wantMissing, recon.MissingIndexes())

	// Respond with all missing transactions.
	req := recon.GetBlockTxnRequest()
	resp := wire.NewMsgBlockTxn(&req.BlockHash)
	for _, idx := range req.Indexes {
		err := resp.AddTransaction(block.MsgBlock().Transactions[idx])
		require.NoError(t, err)
	}

	reconstructed, err := recon.ProvideBlockTxn(resp)
	require.NoError(t, err)
	require.Equal(t, block.Hash(), reconstructed.Hash())
}

// TestCompactBlockReconstructorWitness verifies that version 2 compact blocks
// use the witness transaction ID (wtxid) for short ID computation rather than
// the plain txid, as required by BIP 152.
func TestCompactBlockReconstructorWitness(t *testing.T) {
	block := makeCompactBlockTestBlockWithWitness(3)

	// Build with witness=true (v2).
	msg, err := BuildCompactBlock(block, 55, nil, true)
	require.NoError(t, err)

	// Building with witness=false must produce DIFFERENT short IDs for txns
	// that have witnesses, because txid ≠ wtxid for segwit transactions.
	msgNoWitness, err := BuildCompactBlock(block, 55, nil, false)
	require.NoError(t, err)
	require.NotEqual(t, msg.ShortIDs, msgNoWitness.ShortIDs)

	// Reconstruct using witness=true and the block's own transactions as
	// the mempool source.
	recon, err := NewCompactBlockReconstructor(
		msg, block.Transactions()[1:], true,
	)
	require.NoError(t, err)
	require.True(t, recon.IsComplete(), 
		"reconstructor should be complete, missing %v", recon.MissingIndexes())

	reconstructed, err := recon.Block()
	require.NoError(t, err)
	require.Equal(t, block.Hash(), reconstructed.Hash())
}

// TestShortIDCollisionInMempool verifies that when two mempool transactions
// produce the same short ID (collision), neither is used to fill the block
// slot and the index is reported as missing.  This tests the tolerance
// requirement in BIP 152 §blocktxn: "nodes MUST NOT be penalized for such
// collisions, wherever they appear."
func TestShortIDCollisionInMempool(t *testing.T) {
	// Build a 3-tx block (coinbase prefilled, tx[1] and tx[2] as short IDs).
	block := makeCompactBlockTestBlock(3)
	const nonce = uint64(99)

	msg, err := BuildCompactBlock(block, nonce, nil, false)
	require.NoError(t, err)

	// Obtain the short ID assigned to tx[1].
	shortIDForTx1 := msg.ShortIDs[0]

	tx1 := block.Transactions()[1]
	tx2 := block.Transactions()[2]

	_ = shortIDForTx1

	// Mempool has tx[1] twice and tx[2] once.  The duplicate tx[1] should
	// cancel both entries (collision), leaving only tx[2] resolved.
	mempoolTxns := []*btcutil.Tx{tx1, tx1, tx2}

	recon, err := NewCompactBlockReconstructor(msg, mempoolTxns, false)
	require.NoError(t, err)

	wantMissing := []uint32{1}
	require.Equal(t, wantMissing, recon.MissingIndexes())

	// The node must be able to recover by requesting the missing tx.
	req := recon.GetBlockTxnRequest()
	resp := wire.NewMsgBlockTxn(&req.BlockHash)
	err = resp.AddTransaction(block.MsgBlock().Transactions[1])
	require.NoError(t, err)

	reconstructed, err := recon.ProvideBlockTxn(resp)
	require.NoError(t, err)
	require.Equal(t, block.Hash(), reconstructed.Hash())
}
