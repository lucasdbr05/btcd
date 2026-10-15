package bitcoinkernel

import "errors"

var (
	// ErrUnavailable indicates that btcd was built without libbitcoinkernel
	// support.
	ErrUnavailable = errors.New("libbitcoinkernel support is not compiled")

	// ErrInvalidChain indicates that the requested chain is unsupported.
	ErrInvalidChain = errors.New("invalid libbitcoinkernel chain")

	// ErrInvalidDataDir indicates that the chainstate data directory is
	// missing or invalid.
	ErrInvalidDataDir = errors.New("invalid libbitcoinkernel data directory")

	// ErrInvalidBlocksDir indicates that the block data directory is missing
	// or invalid.
	ErrInvalidBlocksDir = errors.New("invalid libbitcoinkernel blocks directory")

	// ErrInvalidWorkerThreads indicates an out-of-range worker count.
	ErrInvalidWorkerThreads = errors.New("invalid libbitcoinkernel worker thread count")

	// ErrInvalidDBCache indicates that a non-zero database cache is smaller
	// than the minimum accepted by libbitcoinkernel.
	ErrInvalidDBCache = errors.New("invalid libbitcoinkernel database cache size")

	// ErrCreateChainParameters indicates that the native chain parameters
	// could not be allocated.
	ErrCreateChainParameters = errors.New("unable to create libbitcoinkernel chain parameters")

	// ErrCreateContextOptions indicates that the native context options could
	// not be allocated.
	ErrCreateContextOptions = errors.New("unable to create libbitcoinkernel context options")

	// ErrCreateCallbackData indicates that callback state could not be
	// allocated on the C heap.
	ErrCreateCallbackData = errors.New("unable to create libbitcoinkernel callback data")

	// ErrCreateContext indicates that the native kernel context could not be
	// created.
	ErrCreateContext = errors.New("unable to create libbitcoinkernel context")

	// ErrCreateChainstateOptions indicates that the native chainstate manager
	// options could not be created.
	ErrCreateChainstateOptions = errors.New("unable to create libbitcoinkernel chainstate manager options")

	// ErrSetDBCache indicates that the kernel rejected the configured cache.
	ErrSetDBCache = errors.New("unable to set libbitcoinkernel database cache size")

	// ErrCreateChainstate indicates that the native chainstate manager could
	// not be created.
	ErrCreateChainstate = errors.New("unable to create libbitcoinkernel chainstate manager")

	// ErrInterrupt indicates that the native context could not be interrupted.
	ErrInterrupt = errors.New("unable to interrupt libbitcoinkernel context")

	// ErrClosed indicates an operation on a closed kernel.
	ErrClosed = errors.New("libbitcoinkernel is closed")
)
