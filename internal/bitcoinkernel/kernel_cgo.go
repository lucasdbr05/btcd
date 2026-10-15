//go:build libbitcoinkernel && cgo

package bitcoinkernel

/*
#cgo pkg-config: libbitcoinkernel
#include "bridge.h"
#include <stdlib.h>
*/
import "C"

import (
	"runtime"
	"runtime/cgo"
	"unsafe"
)

type nativeKernelBackend struct {
	context   *C.btck_Context
	manager   *C.btck_ChainstateManager
	callbacks *callbackState
}

func openKernelBackend(cfg Config) (kernelBackend, error) {
	chainParams := C.btck_chain_parameters_create(nativeChain(cfg.Chain))
	if chainParams == nil {
		return nil, ErrCreateChainParameters
	}
	defer C.btck_chain_parameters_destroy(chainParams)

	contextOptions := C.btck_context_options_create()
	if contextOptions == nil {
		return nil, ErrCreateContextOptions
	}
	defer C.btck_context_options_destroy(contextOptions)

	C.btck_context_options_set_chainparams(contextOptions, chainParams)

	callbacks := newCallbackState()
	if err := setNativeCallbacks(contextOptions, callbacks); err != nil {
		return nil, err
	}

	context := C.btck_context_create(contextOptions)
	if context == nil {
		return nil, ErrCreateContext
	}
	contextOwned := true
	defer func() {
		if contextOwned {
			C.btck_context_destroy(context)
		}
	}()

	managerOptions, err := createManagerOptions(context, cfg)
	if err != nil {
		return nil, err
	}
	defer C.btck_chainstate_manager_options_destroy(managerOptions)

	manager := C.btck_chainstate_manager_create(managerOptions)
	if manager == nil {
		return nil, ErrCreateChainstate
	}

	contextOwned = false

	return &nativeKernelBackend{
		context:   context,
		manager:   manager,
		callbacks: callbacks,
	}, nil
}

func setNativeCallbacks(contextOptions *C.btck_ContextOptions,
	state *callbackState) error {

	validationHandle := cgo.NewHandle(state)
	var validationCallbacks C.btck_ValidationInterfaceCallbacks
	if C.btcd_kernel_make_validation_callbacks(
		C.uintptr_t(validationHandle), &validationCallbacks,
	) == 0 {

		validationHandle.Delete()
		return ErrCreateCallbackData
	}
	C.btck_context_options_set_validation_interface(
		contextOptions, validationCallbacks,
	)

	notificationHandle := cgo.NewHandle(state)
	var notificationCallbacks C.btck_NotificationInterfaceCallbacks
	if C.btcd_kernel_make_notification_callbacks(
		C.uintptr_t(notificationHandle), &notificationCallbacks,
	) == 0 {

		notificationHandle.Delete()
		return ErrCreateCallbackData
	}
	C.btck_context_options_set_notifications(
		contextOptions, notificationCallbacks,
	)

	return nil
}

func createManagerOptions(context *C.btck_Context,
	cfg Config) (*C.btck_ChainstateManagerOptions, error) {

	dataDir := C.CString(cfg.DataDir)
	defer C.free(unsafe.Pointer(dataDir))

	blocksDir := C.CString(cfg.BlocksDir)
	defer C.free(unsafe.Pointer(blocksDir))

	options := C.btck_chainstate_manager_options_create(
		context,
		dataDir, C.size_t(len(cfg.DataDir)),
		blocksDir, C.size_t(len(cfg.BlocksDir)),
	)
	if options == nil {
		return nil, ErrCreateChainstateOptions
	}

	C.btck_chainstate_manager_options_set_worker_threads_num(
		options, C.int(cfg.WorkerThreads),
	)
	if cfg.DBCacheBytes != 0 &&
		C.btck_chainstate_manager_options_set_database_cache_bytes(
			options, C.uint64_t(cfg.DBCacheBytes),
		) != 0 {

		C.btck_chainstate_manager_options_destroy(options)
		return nil, ErrSetDBCache
	}

	return options, nil
}

func nativeChain(chain Chain) C.btck_ChainType {
	switch chain {
	case Mainnet:
		return C.btck_ChainType_MAINNET
	case Testnet:
		return C.btck_ChainType_TESTNET
	case Testnet4:
		return C.btck_ChainType_TESTNET_4
	case Signet:
		return C.btck_ChainType_SIGNET
	case Regtest:
		return C.btck_ChainType_REGTEST
	default:
		// Config validation prevents this path.
		panic("unsupported libbitcoinkernel chain")
	}
}

func (b *nativeKernelBackend) processBlock(rawBlock []byte) (BlockResult, error) {
	if b.context == nil || b.manager == nil {
		return BlockResult{}, ErrClosed
	}

	block := C.btck_block_create(
		unsafe.Pointer(unsafe.SliceData(rawBlock)), C.size_t(len(rawBlock)),
	)
	runtime.KeepAlive(rawBlock)
	if block == nil {
		return BlockResult{}, ErrCreateBlock
	}
	defer C.btck_block_destroy(block)

	hash := nativeBlockHash(block)
	resultReady := b.callbacks.expectBlock(hash)
	defer b.callbacks.stopExpectingBlock(hash)

	var newBlock C.int
	processStatus := C.btck_chainstate_manager_process_block(
		b.manager, block, &newBlock,
	)
	if processStatus != 0 {
		// ProcessNewBlock also reports consensus-invalid blocks through its
		// return code.  Prefer the detailed block_checked result whenever it
		// was emitted; absence of that callback indicates an operational
		// failure in the native call.
		select {
		case result := <-resultReady:
			result.NewBlock = newBlock != 0
			return result, nil
		default:
			return BlockResult{Hash: hash}, ErrProcessBlock
		}
	}

	if newBlock == 0 {
		select {
		case result := <-resultReady:
			result.NewBlock = false
			return result, nil
		default:
			return BlockResult{Hash: hash}, nil
		}
	}

	result := <-resultReady
	result.NewBlock = true
	return result, nil
}

func (b *nativeKernelBackend) interrupt() error {
	if b.context == nil {
		return ErrClosed
	}

	if C.btck_context_interrupt(b.context) != 0 {
		return ErrInterrupt
	}

	return nil
}

func (b *nativeKernelBackend) close() error {
	if b.context == nil {
		return nil
	}

	interruptErr := b.interrupt()
	if b.manager != nil {
		C.btck_chainstate_manager_destroy(b.manager)
		b.manager = nil
	}
	C.btck_context_destroy(b.context)
	b.context = nil

	return interruptErr
}
