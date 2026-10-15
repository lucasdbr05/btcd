//go:build libbitcoinkernel && cgo

package bitcoinkernel

import (
	"os"
	"testing"
	"time"
)

func TestKernelLifecycle(t *testing.T) {
	tests := []Chain{Mainnet, Testnet, Testnet4, Signet, Regtest}
	for _, chain := range tests {
		chain := chain
		t.Run(chain.String(), func(t *testing.T) {
			cfg := testConfig(t, chain)
			kernel, err := Open(cfg)
			if err != nil {
				t.Fatalf("unable to open kernel: %v", err)
			}

			backend := kernel.backend.(*nativeKernelBackend)
			callbacks := backend.callbacks
			if backend.manager == nil {
				t.Fatal("open kernel has no chainstate manager")
			}
			if got := callbacks.destroyedHandles.Load(); got != 0 {
				t.Fatalf("callback handles destroyed before close: %d", got)
			}
			select {
			case <-callbacks.chainEventReady:
			case <-time.After(5 * time.Second):
				t.Fatal("timed out waiting for initial chain callback")
			}
			callbacks.mu.Lock()
			initialChainEvent := *callbacks.lastChainEvent
			callbacks.mu.Unlock()
			if initialChainEvent.Type != BlockTipUpdated ||
				initialChainEvent.Height != 0 {

				t.Fatalf("unexpected initial chain event: %+v",
					initialChainEvent)
			}
			assertDirectoryExists(t, cfg.DataDir)
			assertDirectoryExists(t, cfg.BlocksDir)

			if err := kernel.Close(); err != nil {
				t.Fatalf("unable to close kernel: %v", err)
			}
			if got := callbacks.destroyedHandles.Load(); got != 2 {
				t.Fatalf("unexpected destroyed callback handles: got %d, want 2", got)
			}
			if err := kernel.Close(); err != nil {
				t.Fatalf("unable to close kernel twice: %v", err)
			}
		})
	}
}

func TestKernelInterrupt(t *testing.T) {
	cfg := testConfig(t, Regtest)
	cfg.WorkerThreads = 2
	cfg.DBCacheBytes = minDatabaseCacheBytes

	kernel, err := Open(cfg)
	if err != nil {
		t.Fatalf("unable to open kernel: %v", err)
	}
	defer kernel.Close()

	if err := kernel.Interrupt(); err != nil {
		t.Fatalf("unable to interrupt kernel: %v", err)
	}
}

func assertDirectoryExists(t *testing.T, path string) {
	t.Helper()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("kernel directory was not created: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("kernel path is not a directory: %s", path)
	}
}
