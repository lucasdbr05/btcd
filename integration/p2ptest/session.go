// Copyright (c) 2026 The btcsuite developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package p2ptest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/btcsuite/btcd/chaincfg/v2"
	"github.com/btcsuite/btcd/chainhash/v2"
	"github.com/btcsuite/btcd/wire/v2"
)

const (
	defaultMessageLimit    = 1024
	defaultTranscriptLimit = 32
)

var (
	// ErrClosed means the test closed the connection.
	ErrClosed = errors.New("p2p connection closed")

	// ErrMessageLimit means the test did not consume incoming messages fast
	// enough.
	ErrMessageLimit = errors.New("p2p message queue full")
)

type HandshakeMode uint8

const (
	AutomaticHandshake HandshakeMode = iota

	ManualHandshake
)

// Config describes a v1 P2P test connection. Address and Params are required.
// Zero ProtocolVersion, MessageLimit, and TranscriptLimit use defaults.
type Config struct {
	Address         string
	Params          *chaincfg.Params
	Mode            HandshakeMode
	ProtocolVersion uint32
	Services        wire.ServiceFlag
	UserAgent       string
	LastBlock       int32
	DisableRelayTx  bool
	DisableAutoPong bool
	MessageLimit    int
	TranscriptLimit int
}

// Session is one programmable connection to a Bitcoin node. The reader and
// writer are independent; all public methods are safe for concurrent use.
type Session struct {
	conn net.Conn
	cfg  Config
	pver atomic.Uint32

	writeMu    sync.Mutex
	mu         sync.Mutex
	messages   []wire.Message
	transcript []string
	handlers   map[string][]func(wire.Message) error
	notify     chan struct{}
	cause      error
	closeOnce  sync.Once
	readDone   chan struct{}
}

// DialRaw opens a TCP connection for tests that need to send malformed frames
// or control bytes before the normal message decoder can run. The caller owns
// the returned connection.
func DialRaw(ctx context.Context, address string) (net.Conn, error) {
	return (&net.Dialer{}).DialContext(ctx, "tcp", address)
}

// Dial connects to a node. With AutomaticHandshake it returns after the node
// has processed our verack; with ManualHandshake it returns immediately after
// starting the message reader.
func Dial(ctx context.Context, cfg Config) (*Session, error) {
	if cfg.Address == "" || cfg.Params == nil {
		return nil, errors.New("p2p address and chain parameters are required")
	}
	if cfg.Mode != AutomaticHandshake && cfg.Mode != ManualHandshake {
		return nil, fmt.Errorf("unknown P2P handshake mode %d", cfg.Mode)
	}
	if cfg.ProtocolVersion == 0 {
		cfg.ProtocolVersion = wire.ProtocolVersion
	}
	if cfg.MessageLimit <= 0 {
		cfg.MessageLimit = defaultMessageLimit
	}
	if cfg.TranscriptLimit <= 0 {
		cfg.TranscriptLimit = defaultTranscriptLimit
	}
	if cfg.UserAgent == "" {
		cfg.UserAgent = "/p2ptest:0.0.0/"
	}

	conn, err := DialRaw(ctx, cfg.Address)
	if err != nil {
		return nil, fmt.Errorf("dial P2P peer: %w", err)
	}
	s := &Session{
		conn:     conn,
		cfg:      cfg,
		notify:   make(chan struct{}),
		readDone: make(chan struct{}),
	}
	s.pver.Store(cfg.ProtocolVersion)
	go s.readLoop()

	if cfg.Mode == AutomaticHandshake {
		if err := s.Handshake(ctx); err != nil {
			s.Close()
			return nil, err
		}
	}
	return s, nil
}

// VersionMessage constructs the local version message. Manual-handshake tests
// may alter it before sending it with Send.
func (s *Session) VersionMessage() *wire.MsgVersion {
	local := netAddress(s.conn.LocalAddr(), s.cfg.Services)
	remote := netAddress(s.conn.RemoteAddr(), s.cfg.Services)
	msg := wire.NewMsgVersion(local, remote, rand.Uint64(), s.cfg.LastBlock)
	msg.ProtocolVersion = int32(s.cfg.ProtocolVersion)
	msg.Services = s.cfg.Services
	msg.UserAgent = s.cfg.UserAgent
	msg.DisableRelayTx = s.cfg.DisableRelayTx
	return msg
}

func netAddress(addr net.Addr, services wire.ServiceFlag) *wire.NetAddress {
	if tcp, ok := addr.(*net.TCPAddr); ok {
		return wire.NewNetAddress(tcp, services)
	}
	return wire.NewNetAddressIPPort(net.IPv4zero, 0, services)
}

// Handshake performs version/verack and then uses a ping/pong barrier to
// ensure the remote node has processed our verack. Call it at most once.
func (s *Session) Handshake(ctx context.Context) error {
	if err := s.Send(ctx, s.VersionMessage()); err != nil {
		return fmt.Errorf("send version: %w", err)
	}
	msg, err := s.WaitForCommand(ctx, wire.CmdVersion)
	if err != nil {
		return fmt.Errorf("receive version: %w", err)
	}
	remote := msg.(*wire.MsgVersion)
	if remote.ProtocolVersion > 0 && uint32(remote.ProtocolVersion) < s.pver.Load() {
		s.pver.Store(uint32(remote.ProtocolVersion))
	}
	if err := s.Send(ctx, wire.NewMsgVerAck()); err != nil {
		return fmt.Errorf("send verack: %w", err)
	}
	if _, err := s.WaitForCommand(ctx, wire.CmdVerAck); err != nil {
		return fmt.Errorf("receive verack: %w", err)
	}
	return s.Sync(ctx)
}

// Send writes the complete message or returns an error. Unlike an asynchronous
// send queue, success means the bytes were written to the socket.
func (s *Session) Send(ctx context.Context, msg wire.Message) error {
	if msg == nil {
		return errors.New("nil P2P message")
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := ctx.Err(); err != nil {
		return s.wrap("send "+msg.Command(), err)
	}
	if err := s.connectionError(); err != nil {
		return s.wrap("send "+msg.Command(), err)
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = s.conn.SetWriteDeadline(deadline)
	}
	cancelledWrite := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		_ = s.conn.SetWriteDeadline(time.Now())
		close(cancelledWrite)
	})
	s.record("send " + msg.Command())
	_, err := wire.WriteMessageWithEncodingN(
		s.conn, msg, s.pver.Load(), s.cfg.Params.Net, wire.WitnessEncoding,
	)
	if !stop() {
		<-cancelledWrite
	}
	_ = s.conn.SetWriteDeadline(time.Time{})
	if err != nil {
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		s.record("send error " + msg.Command() + ": " + err.Error())
		s.terminate(err)
		return s.wrap("send "+msg.Command(), err)
	}
	return nil
}

// Sync waits for a pong with a unique nonce. It confirms the remote peer
// processed the ping, but does not guarantee unrelated asynchronous work has
// completed.
func (s *Session) Sync(ctx context.Context) error {
	nonce := rand.Uint64()
	if err := s.Send(ctx, wire.NewMsgPing(nonce)); err != nil {
		return err
	}
	_, err := s.WaitFor(ctx, func(msg wire.Message) bool {
		pong, ok := msg.(*wire.MsgPong)
		return ok && pong.Nonce == nonce
	})
	return err
}

// SendAndSync sends a message, then waits for a subsequent ping response. It
// does not assert a particular response to the first message.
func (s *Session) SendAndSync(ctx context.Context, msg wire.Message) error {
	if err := s.Send(ctx, msg); err != nil {
		return err
	}
	return s.Sync(ctx)
}

// WaitFor returns and consumes the first queued message matching predicate.
// Unmatched messages remain queued. A nil predicate matches the next message.
// The predicate must not call Session methods because it runs under a lock.
func (s *Session) WaitFor(ctx context.Context,
	predicate func(wire.Message) bool) (wire.Message, error) {

	for {
		msg, cause, notify := s.takeMatching(predicate)
		if msg != nil {
			return msg, nil
		}
		if cause != nil {
			return nil, s.wrap("wait for message", cause)
		}
		select {
		case <-notify:
		case <-ctx.Done():
			return nil, s.wrap("wait for message", ctx.Err())
		}
	}
}

func (s *Session) takeMatching(predicate func(wire.Message) bool) (
	wire.Message, error, <-chan struct{}) {

	s.mu.Lock()
	defer s.mu.Unlock()
	for i, msg := range s.messages {
		if predicate == nil || predicate(msg) {
			copy(s.messages[i:], s.messages[i+1:])
			s.messages[len(s.messages)-1] = nil
			s.messages = s.messages[:len(s.messages)-1]
			return msg, nil, nil
		}
	}
	return nil, s.cause, s.notify
}

// WaitForCommand waits for the next message with the given wire command.
func (s *Session) WaitForCommand(ctx context.Context, command string) (wire.Message, error) {
	return s.WaitFor(ctx, func(msg wire.Message) bool {
		return msg.Command() == command
	})
}

// On registers a callback for incoming messages with command. A callback runs
// on the reader goroutine, in arrival order, after the message has been queued.
func (s *Session) On(command string, handler func(wire.Message) error) {
	if handler == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.handlers == nil {
		s.handlers = make(map[string][]func(wire.Message) error)
	}
	s.handlers[command] = append(s.handlers[command], handler)
}

// WaitForInv waits for an inventory announcement containing hash, then
// returns a copy of the matching inventory vector.
func (s *Session) WaitForInv(ctx context.Context, hash chainhash.Hash) (*wire.InvVect, error) {
	msg, err := s.WaitFor(ctx, func(msg wire.Message) bool {
		inv, ok := msg.(*wire.MsgInv)
		if !ok {
			return false
		}
		for _, item := range inv.InvList {
			if item.Hash == hash {
				return true
			}
		}
		return false
	})
	if err != nil {
		return nil, err
	}
	for _, item := range msg.(*wire.MsgInv).InvList {
		if item.Hash == hash {
			copy := *item
			return &copy, nil
		}
	}
	return nil, s.wrap("find inventory", errors.New("matching inventory disappeared"))
}

// Transcript returns a bounded chronological summary of sent and received
// messages. Wait errors include this summary for diagnosis.
func (s *Session) Transcript() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.transcript...)
}

// Close disconnects and waits for the reader goroutine. It is idempotent.
func (s *Session) Close() error {
	s.terminate(ErrClosed)
	<-s.readDone
	return nil
}

func (s *Session) readLoop() {
	defer close(s.readDone)
	for {
		_, msg, _, err := wire.ReadMessageWithEncodingN(
			s.conn, s.pver.Load(), s.cfg.Params.Net, wire.WitnessEncoding,
		)
		if err != nil {
			if errors.Is(err, io.EOF) {
				err = io.EOF
			}
			s.terminate(err)
			return
		}
		s.mu.Lock()
		if s.cause != nil {
			s.mu.Unlock()
			return
		}
		if len(s.messages) >= s.cfg.MessageLimit {
			s.mu.Unlock()
			s.terminate(ErrMessageLimit)
			return
		}
		s.messages = append(s.messages, msg)
		s.recordLocked("recv " + msg.Command())
		handlers := append([]func(wire.Message) error{}, s.handlers[msg.Command()]...)
		close(s.notify)
		s.notify = make(chan struct{})
		s.mu.Unlock()
		for _, handler := range handlers {
			if err := runHandler(handler, msg); err != nil {
				s.terminate(fmt.Errorf("P2P %s callback: %w", msg.Command(), err))
				return
			}
		}

		if ping, ok := msg.(*wire.MsgPing); ok && !s.cfg.DisableAutoPong {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			err := s.Send(ctx, wire.NewMsgPong(ping.Nonce))
			cancel()
			if err != nil {
				return
			}
		}
	}
}

func runHandler(handler func(wire.Message) error, msg wire.Message) (err error) {
	defer func() {
		if value := recover(); value != nil {
			err = fmt.Errorf("panic: %v", value)
		}
	}()
	return handler(msg)
}

func (s *Session) terminate(err error) {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.cause = err
		close(s.notify)
		s.mu.Unlock()
		_ = s.conn.Close()
	})
}

func (s *Session) connectionError() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cause
}

func (s *Session) record(event string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recordLocked(event)
}

func (s *Session) recordLocked(event string) {
	if len(s.transcript) == s.cfg.TranscriptLimit {
		copy(s.transcript, s.transcript[1:])
		s.transcript = s.transcript[:len(s.transcript)-1]
	}
	s.transcript = append(s.transcript, event)
}

func (s *Session) wrap(action string, err error) error {
	return fmt.Errorf("%s: %w (recent P2P: %s)", action, err,
		strings.Join(s.Transcript(), ", "))
}
