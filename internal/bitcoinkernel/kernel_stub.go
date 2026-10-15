//go:build !libbitcoinkernel || !cgo

package bitcoinkernel

func openKernelBackend(Config) (kernelBackend, error) {
	return nil, ErrUnavailable
}
