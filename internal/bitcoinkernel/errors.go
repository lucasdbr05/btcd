package bitcoinkernel

import "errors"

var (
	// ErrUnavailable indicates that btcd was built without libbitcoinkernel
	// support.
	ErrUnavailable = errors.New("libbitcoinkernel support is not compiled")

	// ErrInvalidChain indicates that the requested chain is unsupported.
	ErrInvalidChain = errors.New("invalid libbitcoinkernel chain")

	// ErrCreateChainParameters indicates that the native chain parameters
	// could not be allocated.
	ErrCreateChainParameters = errors.New("unable to create libbitcoinkernel chain parameters")

	// ErrCreateContextOptions indicates that the native context options could
	// not be allocated.
	ErrCreateContextOptions = errors.New("unable to create libbitcoinkernel context options")

	// ErrCreateContext indicates that the native kernel context could not be
	// created.
	ErrCreateContext = errors.New("unable to create libbitcoinkernel context")
)
