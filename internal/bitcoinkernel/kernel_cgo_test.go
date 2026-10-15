//go:build libbitcoinkernel && cgo

package bitcoinkernel

import "testing"

func TestKernelLifecycle(t *testing.T) {
	tests := []Chain{Mainnet, Testnet, Testnet4, Signet, Regtest}
	for _, chain := range tests {
		chain := chain
		t.Run(chain.String(), func(t *testing.T) {
			kernel, err := Open(Config{Chain: chain})
			if err != nil {
				t.Fatalf("unable to open kernel: %v", err)
			}

			if err := kernel.Close(); err != nil {
				t.Fatalf("unable to close kernel: %v", err)
			}
			if err := kernel.Close(); err != nil {
				t.Fatalf("unable to close kernel twice: %v", err)
			}
		})
	}
}
