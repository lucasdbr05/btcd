//go:build rpctest
// +build rpctest

package integration

import (
	"context"
	"encoding/hex"
	"testing"
	"time"

	"github.com/btcsuite/btcd/chaincfg/v2"
	"github.com/btcsuite/btcd/integration/p2ptest"
	"github.com/btcsuite/btcd/integration/rpctest"
	"github.com/btcsuite/btcd/txscript/v2"
	"github.com/btcsuite/btcd/wire/v2"
	"github.com/stretchr/testify/require"
)

// TestTransactionPropagationAndConfirmation verifies that a transaction sent
// to one node is announced over P2P, reaches the other node, is mined by that
// node, and leaves both nodes with the same unspent output.
func TestTransactionPropagationAndConfirmation(t *testing.T) {
	sender, err := rpctest.New(&chaincfg.SimNetParams, nil, nil, "")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sender.TearDown()) })
	require.NoError(t, sender.SetUp(true, 1))

	receiver, err := rpctest.New(&chaincfg.SimNetParams, nil, nil, "")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, receiver.TearDown()) })
	require.NoError(t, receiver.SetUp(false, 0))

	require.NoError(t, rpctest.ConnectNode(receiver, sender))
	senderTip, err := sender.Client.GetBestBlockHash()
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		receiverTip, err := receiver.Client.GetBestBlockHash()
		return err == nil && receiverTip.IsEqual(senderTip)
	}, 30*time.Second, 100*time.Millisecond)

	// Observe the sender's P2P relay without replacing the second full node.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	observer, err := p2ptest.Dial(ctx, p2ptest.Config{
		Address:  sender.P2PAddress(),
		Params:   sender.ActiveNet,
		Services: wire.SFNodeNetwork | wire.SFNodeWitness,
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, observer.Close()) })

	address, err := receiver.NewAddress()
	require.NoError(t, err)
	pkScript, err := txscript.PayToAddrScript(address)
	require.NoError(t, err)
	const amount = 100_000_000
	tx, err := sender.CreateTransaction(
		[]*wire.TxOut{wire.NewTxOut(amount, pkScript)}, 10, true,
	)
	require.NoError(t, err)
	txHash, err := sender.Client.SendRawTransaction(tx, true)
	require.NoError(t, err)
	require.Equal(t, tx.TxHash(), *txHash)

	// The observer must receive an inventory announcement, then be able to
	// request the actual transaction from the sender.
	inv, err := observer.WaitForInv(ctx, *txHash)
	require.NoError(t, err)
	require.Equal(t, wire.InvTypeTx, inv.Type)

	getData := wire.NewMsgGetData()
	require.NoError(t, getData.AddInvVect(inv))
	require.NoError(t, observer.Send(ctx, getData))
	msg, err := observer.WaitFor(ctx, func(msg wire.Message) bool {
		relayedTx, ok := msg.(*wire.MsgTx)
		return ok && relayedTx.TxHash() == *txHash
	})
	require.NoError(t, err)
	require.Equal(t, tx.TxHash(), msg.(*wire.MsgTx).TxHash())

	// The sender broadcasts the transaction through RPC, and the receiver
	// must learn about it through the peer connection.
	for _, node := range []*rpctest.Harness{sender, receiver} {
		require.Eventually(t, func() bool {
			pool, err := node.Client.GetRawMempool()
			return err == nil && len(pool) == 1 && pool[0].IsEqual(txHash)
		}, 30*time.Second, 100*time.Millisecond)
	}

	blockHashes, err := receiver.Client.Generate(1)
	require.NoError(t, err)
	require.Len(t, blockHashes, 1)
	blockHash := blockHashes[0]
	require.Eventually(t, func() bool {
		senderBest, err := sender.Client.GetBestBlockHash()
		return err == nil && senderBest.IsEqual(blockHash)
	}, 30*time.Second, 100*time.Millisecond)

	for _, node := range []*rpctest.Harness{sender, receiver} {
		block, err := node.Client.GetBlock(blockHash)
		require.NoError(t, err)
		require.Len(t, block.Transactions, 2)
		require.Equal(t, *txHash, block.Transactions[1].TxHash())

		pool, err := node.Client.GetRawMempool()
		require.NoError(t, err)
		require.Empty(t, pool)

		utxo, err := node.Client.GetTxOut(txHash, 0, false)
		require.NoError(t, err)
		require.NotNil(t, utxo)
		require.Equal(t, blockHash.String(), utxo.BestBlock)
		require.Equal(t, int64(1), utxo.Confirmations)
		require.Equal(t, float64(amount)/1e8, utxo.Value)
		require.Equal(t, hex.EncodeToString(pkScript), utxo.ScriptPubKey.Hex)
		require.False(t, utxo.Coinbase)
	}
}
