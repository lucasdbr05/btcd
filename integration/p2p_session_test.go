//go:build rpctest
// +build rpctest

package integration

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/btcsuite/btcd/chaincfg/v2"
	"github.com/btcsuite/btcd/integration/p2ptest"
	"github.com/btcsuite/btcd/integration/rpctest"
	"github.com/btcsuite/btcd/wire/v2"
	"github.com/stretchr/testify/require"
)

// TestP2PManualHandshake checks that a test can control each handshake step
// instead of having the session complete it on connection.
func TestP2PManualHandshake(t *testing.T) {
	harness, err := rpctest.New(&chaincfg.SimNetParams, nil, nil, "")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, harness.TearDown()) })
	require.NoError(t, harness.SetUp(false, 0))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	p, err := p2ptest.Dial(ctx, p2ptest.Config{
		Address:  harness.P2PAddress(),
		Params:   harness.ActiveNet,
		Mode:     p2ptest.ManualHandshake,
		Services: wire.SFNodeNetwork | wire.SFNodeWitness,
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, p.Close()) })

	require.NoError(t, p.Send(ctx, p.VersionMessage()))
	_, err = p.WaitForCommand(ctx, wire.CmdVersion)
	require.NoError(t, err)
	require.NoError(t, p.Send(ctx, wire.NewMsgVerAck()))
	_, err = p.WaitForCommand(ctx, wire.CmdVerAck)
	require.NoError(t, err)
	require.NoError(t, p.Sync(ctx))

	// Callbacks can react to later messages without consuming the wait queue.
	pongs := make(chan uint64, 1)
	p.On(wire.CmdPong, func(msg wire.Message) error {
		pongs <- msg.(*wire.MsgPong).Nonce
		return nil
	})
	const nonce = 67890
	require.NoError(t, p.Send(ctx, wire.NewMsgPing(nonce)))
	select {
	case got := <-pongs:
		require.Equal(t, uint64(nonce), got)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}

	callbackErr := errors.New("callback rejected pong")
	p.On(wire.CmdPong, func(wire.Message) error { return callbackErr })
	require.NoError(t, p.Send(ctx, wire.NewMsgPing(nonce+1)))
	_, err = p.WaitFor(ctx, func(wire.Message) bool { return false })
	require.ErrorIs(t, err, callbackErr)
	require.Contains(t, err.Error(), "recv pong")
}
