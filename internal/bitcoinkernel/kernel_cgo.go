//go:build libbitcoinkernel && cgo

package bitcoinkernel

/*
#cgo pkg-config: libbitcoinkernel
#include <bitcoinkernel.h>
*/
import "C"

type nativeKernelBackend struct {
	context *C.btck_Context
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

	context := C.btck_context_create(contextOptions)
	if context == nil {
		return nil, ErrCreateContext
	}

	return &nativeKernelBackend{context: context}, nil
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

func (b *nativeKernelBackend) close() {
	if b.context == nil {
		return
	}

	C.btck_context_destroy(b.context)
	b.context = nil
}
