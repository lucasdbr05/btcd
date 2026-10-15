package p2ptest

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/btcsuite/btcd/chaincfg/v2"
	"github.com/btcsuite/btcd/wire/v2"
)

func TestSessionObservationsAndSemanticWaits(t *testing.T) {
	session, remote := newPipeSession(t)
	tx := wire.NewMsgTx(wire.TxVersion)
	tx.AddTxIn(wire.NewTxIn(&wire.OutPoint{}, nil, nil))
	tx.AddTxOut(wire.NewTxOut(1, nil))
	block := wire.NewMsgBlock(&wire.BlockHeader{})

	writeDone := make(chan error, 1)
	go func() {
		for _, msg := range []wire.Message{tx, block} {
			_, err := wire.WriteMessageWithEncodingN(
				remote, msg, wire.ProtocolVersion, wire.SimNet,
				wire.WitnessEncoding,
			)
			if err != nil {
				writeDone <- err
				return
			}
		}
		writeDone <- nil
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := session.WaitForCommandCount(ctx, wire.CmdTx, 1); err != nil {
		t.Fatalf("wait for transaction count: %v", err)
	}
	if got := session.MessageCount(wire.CmdTx); got != 1 {
		t.Fatalf("transaction count = %d, want 1", got)
	}
	last, ok := session.LastMessage(wire.CmdTx)
	lastTx, isTx := last.(*wire.MsgTx)
	if !ok || !isTx || lastTx.TxHash() != tx.TxHash() {
		t.Fatalf("last transaction = %#v, found = %v", last, ok)
	}

	gotTx, err := session.WaitForTx(ctx, tx.TxHash())
	if err != nil {
		t.Fatalf("wait for transaction: %v", err)
	}
	if gotTx.TxHash() != tx.TxHash() {
		t.Fatalf("transaction = %s, want %s", gotTx.TxHash(), tx.TxHash())
	}
	gotBlock, err := session.WaitForBlock(ctx, block.BlockHash())
	if err != nil {
		t.Fatalf("wait for block: %v", err)
	}
	if gotBlock.BlockHash() != block.BlockHash() {
		t.Fatalf("block = %s, want %s", gotBlock.BlockHash(), block.BlockHash())
	}
	if err := <-writeDone; err != nil {
		t.Fatalf("write messages: %v", err)
	}
}

func TestSessionCloseFromCallback(t *testing.T) {
	session, remote := newPipeSession(t)
	callbackDone := make(chan error, 1)
	session.On(wire.CmdPing, func(wire.Message) error {
		callbackDone <- session.Close()
		return nil
	})

	writeDone := make(chan error, 1)
	go func() {
		_, err := wire.WriteMessageWithEncodingN(
			remote, wire.NewMsgPing(1), wire.ProtocolVersion, wire.SimNet,
			wire.WitnessEncoding,
		)
		writeDone <- err
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	select {
	case err := <-callbackDone:
		if err != nil {
			t.Fatalf("close in callback: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("close in callback blocked")
	}
	if err := session.WaitForDisconnect(ctx); !errors.Is(err, ErrClosed) {
		t.Fatalf("disconnect error = %v, want ErrClosed", err)
	}
	if state := session.State(); state != Closed {
		t.Fatalf("state = %v, want Closed", state)
	}
	if err := <-writeDone; err != nil {
		t.Fatalf("write ping: %v", err)
	}
}

func TestHandshakeStatus(t *testing.T) {
	session, remote := newPipeSession(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	readVersion := make(chan error, 1)
	go func() {
		_, msg, _, err := wire.ReadMessageWithEncodingN(
			remote, wire.ProtocolVersion, wire.SimNet, wire.WitnessEncoding,
		)
		if err == nil && msg.Command() != wire.CmdVersion {
			err = errors.New("expected version")
		}
		readVersion <- err
	}()
	if err := session.Send(ctx, session.VersionMessage()); err != nil {
		t.Fatalf("send version: %v", err)
	}
	if err := <-readVersion; err != nil {
		t.Fatalf("read version: %v", err)
	}

	writeDone := make(chan error, 1)
	go func() {
		for _, msg := range []wire.Message{
			session.VersionMessage(), wire.NewMsgVerAck(),
		} {
			_, err := wire.WriteMessageWithEncodingN(
				remote, msg, wire.ProtocolVersion, wire.SimNet,
				wire.WitnessEncoding,
			)
			if err != nil {
				writeDone <- err
				return
			}
		}
		_, msg, _, err := wire.ReadMessageWithEncodingN(
			remote, wire.ProtocolVersion, wire.SimNet, wire.WitnessEncoding,
		)
		if err == nil && msg.Command() != wire.CmdVerAck {
			err = errors.New("expected verack")
		}
		if err != nil {
			writeDone <- err
			return
		}
		writeDone <- nil
	}()
	if _, err := session.WaitForCommand(ctx, wire.CmdVersion); err != nil {
		t.Fatalf("wait for version: %v", err)
	}
	if err := session.Send(ctx, wire.NewMsgVerAck()); err != nil {
		t.Fatalf("send verack: %v", err)
	}
	if _, err := session.WaitForCommand(ctx, wire.CmdVerAck); err != nil {
		t.Fatalf("wait for verack: %v", err)
	}
	if err := <-writeDone; err != nil {
		t.Fatalf("write handshake: %v", err)
	}
	if status := session.HandshakeStatus(); !status.Complete() {
		t.Fatalf("handshake status = %+v, want complete", status)
	}
}

func newPipeSession(t *testing.T) (*Session, net.Conn) {
	t.Helper()
	remote, conn := net.Pipe()
	session := &Session{
		conn: conn,
		cfg: Config{
			Params:          &chaincfg.SimNetParams,
			MessageLimit:    8,
			TranscriptLimit: 8,
			DisableAutoPong: true,
		},
		notify:   make(chan struct{}),
		readDone: make(chan struct{}),
		counts:   make(map[string]uint64),
		last:     make(map[string]wire.Message),
	}
	session.pver.Store(wire.ProtocolVersion)
	go session.readLoop()
	t.Cleanup(func() {
		_ = session.Close()
		_ = remote.Close()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := session.WaitForDisconnect(ctx); err == nil {
			t.Error("wait for disconnect returned nil")
		}
	})
	return session, remote
}
