//go:build !libbitcoinkernel || !cgo

package bitcoinkernel

import (
	"errors"
	"testing"
)

func TestKernelUnavailable(t *testing.T) {
	t.Parallel()

	kernel, err := Open(testConfig(t, Regtest))
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("unexpected error: got %v, want %v", err, ErrUnavailable)
	}
	if kernel != nil {
		t.Fatal("unavailable backend returned a kernel")
	}
}
