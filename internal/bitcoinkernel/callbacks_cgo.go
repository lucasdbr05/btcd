//go:build libbitcoinkernel && cgo

package bitcoinkernel

/*
#cgo pkg-config: libbitcoinkernel
#include "bridge.h"
*/
import "C"

import (
	"runtime/cgo"
)

func callbackStateFromHandle(rawHandle C.uintptr_t) *callbackState {
	handle := cgo.Handle(uintptr(rawHandle))
	return handle.Value().(*callbackState)
}

func callbackGuard(callback func()) {
	defer func() {
		_ = recover()
	}()

	callback()
}

//export goKernelDestroyHandle
func goKernelDestroyHandle(rawHandle C.uintptr_t) {
	callbackGuard(func() {
		handle := cgo.Handle(uintptr(rawHandle))
		state := handle.Value().(*callbackState)
		state.destroyedHandles.Add(1)
		handle.Delete()
	})
}

//export goKernelBlockChecked
func goKernelBlockChecked(rawHandle C.uintptr_t, block *C.btck_Block,
	validationState *C.btck_BlockValidationState) {

	callbackGuard(func() {
		if block == nil || validationState == nil {
			return
		}

		state := callbackStateFromHandle(rawHandle)
		state.recordBlockResult(BlockResult{
			Hash:   nativeBlockHash(block),
			Mode:   ValidationMode(C.btck_block_validation_state_get_validation_mode(validationState)),
			Reason: BlockValidationResult(C.btck_block_validation_state_get_block_validation_result(validationState)),
		})
	})
}

//export goKernelBlockConnected
func goKernelBlockConnected(rawHandle C.uintptr_t, block *C.btck_Block,
	entry *C.btck_BlockTreeEntry) {

	recordNativeChainEvent(rawHandle, entry, BlockConnected)
}

//export goKernelBlockDisconnected
func goKernelBlockDisconnected(rawHandle C.uintptr_t, block *C.btck_Block,
	entry *C.btck_BlockTreeEntry) {

	recordNativeChainEvent(rawHandle, entry, BlockDisconnected)
}

//export goKernelBlockTip
func goKernelBlockTip(rawHandle C.uintptr_t,
	synchronizationState C.btck_SynchronizationState,
	entry *C.btck_BlockTreeEntry, verificationProgress C.double) {

	recordNativeChainEvent(rawHandle, entry, BlockTipUpdated)
}

//export goKernelFlushError
func goKernelFlushError(rawHandle C.uintptr_t, message *C.char,
	messageLen C.size_t) {

	callbackGuard(func() {
		callbackStateFromHandle(rawHandle).recordFlushError(
			nativeString(message, messageLen),
		)
	})
}

//export goKernelFatalError
func goKernelFatalError(rawHandle C.uintptr_t, message *C.char,
	messageLen C.size_t) {

	callbackGuard(func() {
		callbackStateFromHandle(rawHandle).recordFatalError(
			nativeString(message, messageLen),
		)
	})
}

func recordNativeChainEvent(rawHandle C.uintptr_t,
	entry *C.btck_BlockTreeEntry, eventType ChainEventType) {

	callbackGuard(func() {
		if entry == nil {
			return
		}

		state := callbackStateFromHandle(rawHandle)
		state.recordChainEvent(ChainEvent{
			Type:   eventType,
			Hash:   nativeEntryHash(entry),
			Height: int32(C.btck_block_tree_entry_get_height(entry)),
		})
	})
}

func nativeBlockHash(block *C.btck_Block) [32]byte {
	var hash [32]byte
	nativeHash := C.btck_block_get_hash(block)
	if nativeHash == nil {
		return hash
	}
	defer C.btck_block_hash_destroy(nativeHash)

	C.btck_block_hash_to_bytes(nativeHash, (*C.uchar)(&hash[0]))
	return hash
}

func nativeEntryHash(entry *C.btck_BlockTreeEntry) [32]byte {
	var hash [32]byte
	nativeHash := C.btck_block_tree_entry_get_block_hash(entry)
	if nativeHash == nil {
		return hash
	}

	C.btck_block_hash_to_bytes(nativeHash, (*C.uchar)(&hash[0]))
	return hash
}

func nativeString(message *C.char, messageLen C.size_t) string {
	if message == nil || messageLen == 0 {
		return ""
	}

	const maxCInt = C.size_t(^uint32(0) >> 1)
	if messageLen > maxCInt {
		messageLen = maxCInt
	}

	return C.GoStringN(message, C.int(messageLen))
}
