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

// BlockResult summarizes a block submission and any block_checked callback.
type BlockResult struct {
	Hash [32]byte

	// Checked reports whether the kernel emitted a block_checked result.
	// Duplicate blocks are accepted by ProcessBlock without necessarily
	// producing that callback.
	Checked bool

	// NewBlock reports the value returned through libbitcoinkernel's
	// new_block output parameter.
	NewBlock bool

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
	pendingBlocks   map[[32]byte]chan BlockResult

	destroyedHandles atomic.Uint32
}

func newCallbackState() *callbackState {
	return &callbackState{
		chainEventReady: make(chan struct{}, 1),
		pendingBlocks:   make(map[[32]byte]chan BlockResult),
	}
}

func (s *callbackState) recordBlockResult(result BlockResult) {
	s.mu.Lock()
	defer s.mu.Unlock()

	result.Checked = true
	s.lastBlockResult = &result

	pending := s.pendingBlocks[result.Hash]
	if pending != nil {
		select {
		case pending <- result:
		default:
		}
	}
}

func (s *callbackState) expectBlock(hash [32]byte) chan BlockResult {
	s.mu.Lock()
	defer s.mu.Unlock()

	result := make(chan BlockResult, 1)
	s.pendingBlocks[hash] = result
	return result
}

func (s *callbackState) stopExpectingBlock(hash [32]byte) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.pendingBlocks, hash)
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
