// Package bitcoinkernel provides the internal Go boundary to Bitcoin Core's
// experimental libbitcoinkernel C API.
package bitcoinkernel

import "sync"

type kernelBackend interface {
	interrupt() error
	close() error
}

// Kernel owns a libbitcoinkernel context.
//
// Callers must call Close when the kernel is no longer needed.  Kernel is safe
// to close concurrently and Close is idempotent.
type Kernel struct {
	mu      sync.Mutex
	backend kernelBackend
}

// Open creates a kernel context for the configured chain.
func Open(cfg Config) (*Kernel, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	backend, err := openKernelBackend(cfg)
	if err != nil {
		return nil, err
	}

	return &Kernel{backend: backend}, nil
}

// Close releases the native kernel context.  Subsequent calls are no-ops.
func (k *Kernel) Close() error {
	if k == nil {
		return nil
	}

	k.mu.Lock()
	defer k.mu.Unlock()

	if k.backend == nil {
		return nil
	}

	err := k.backend.close()
	k.backend = nil

	return err
}

// Interrupt requests cancellation of long-running kernel operations.
func (k *Kernel) Interrupt() error {
	if k == nil {
		return ErrClosed
	}

	k.mu.Lock()
	defer k.mu.Unlock()

	if k.backend == nil {
		return ErrClosed
	}

	return k.backend.interrupt()
}
