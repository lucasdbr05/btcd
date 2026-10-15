package bitcoinkernel

import (
	"sync"
	"sync/atomic"
)

// ValidationMode describes whether validation succeeded, failed, or
// encountered an internal error.
type ValidationMode uint8

// Validation modes returned by libbitcoinkernel.
const (
	ValidationValid ValidationMode = iota
	ValidationInvalid
	ValidationInternalError
)

// BlockValidationResult provides the kernel's granular block result.
type BlockValidationResult uint32

// Block validation results returned by libbitcoinkernel.
const (
	BlockResultUnset BlockValidationResult = iota
	BlockResultConsensus
	BlockResultCachedInvalid
	BlockResultInvalidHeader
	BlockResultMutated
	BlockResultMissingPrevious
	BlockResultInvalidPrevious
	BlockResultTimeFuture
	BlockResultHeaderLowWork
)

// BlockResult is a copied Go representation of a block_checked callback.
type BlockResult struct {
	Hash   [32]byte
	Mode   ValidationMode
	Reason BlockValidationResult
}

// ChainEventType identifies a best-chain transition reported by the kernel.
type ChainEventType uint8

// Chain event types reported by libbitcoinkernel.
const (
	BlockConnected ChainEventType = iota
	BlockDisconnected
	BlockTipUpdated
)

// ChainEvent is a copied Go representation of a kernel chain callback.
type ChainEvent struct {
	Type   ChainEventType
	Hash   [32]byte
	Height int32
}

type callbackState struct {
	mu sync.Mutex

	lastBlockResult *BlockResult
	lastChainEvent  *ChainEvent
	flushError      string
	fatalError      string
	chainEventReady chan struct{}

	destroyedHandles atomic.Uint32
}

func newCallbackState() *callbackState {
	return &callbackState{
		chainEventReady: make(chan struct{}, 1),
	}
}

func (s *callbackState) recordBlockResult(result BlockResult) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.lastBlockResult = &result
}

func (s *callbackState) recordChainEvent(event ChainEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.lastChainEvent = &event
	select {
	case s.chainEventReady <- struct{}{}:
	default:
	}
}

func (s *callbackState) recordFlushError(message string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.flushError = message
}

func (s *callbackState) recordFatalError(message string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.fatalError = message
}
