//go:build libbitcoinkernel && cgo

package bitcoinkernel

import (
	"bytes"
	"errors"
	"math"
	"os"
	"testing"
	"time"

	"github.com/btcsuite/btcd/blockchain"
	"github.com/btcsuite/btcd/chaincfg/v2"
	"github.com/btcsuite/btcd/chainhash/v2"
	"github.com/btcsuite/btcd/wire/v2"
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

func TestProcessBlock(t *testing.T) {
	kernel, err := Open(testConfig(t, Regtest))
	if err != nil {
		t.Fatalf("unable to open kernel: %v", err)
	}
	defer kernel.Close()

	rawBlock, wantHash := makeRegtestBlock(t, 50*1e8)
	result, err := kernel.ProcessBlock(rawBlock)
	if err != nil {
		t.Fatalf("unable to process block: %v", err)
	}
	if !result.Checked {
		t.Fatal("new block did not produce a validation result")
	}
	if result.Mode != ValidationValid || result.Reason != BlockResultUnset {
		t.Fatalf("unexpected validation result: %+v", result)
	}
	if !result.NewBlock {
		t.Fatal("first block submission was reported as a duplicate")
	}
	if result.Hash != wantHash {
		t.Fatalf("unexpected block hash: got %x, want %x", result.Hash,
			wantHash)
	}

	duplicate, err := kernel.ProcessBlock(rawBlock)
	if err != nil {
		t.Fatalf("unable to process duplicate block: %v", err)
	}
	if duplicate.NewBlock {
		t.Fatal("duplicate block was reported as new")
	}
	if duplicate.Hash != wantHash {
		t.Fatalf("unexpected duplicate hash: got %x, want %x",
			duplicate.Hash, wantHash)
	}
}

func TestProcessBlockConsensusRejection(t *testing.T) {
	kernel, err := Open(testConfig(t, Regtest))
	if err != nil {
		t.Fatalf("unable to open kernel: %v", err)
	}
	defer kernel.Close()

	rawBlock, wantHash := makeRegtestBlock(t, 50*1e8+1)
	result, err := kernel.ProcessBlock(rawBlock)
	if err != nil {
		t.Fatalf("consensus rejection returned an operational error: %v", err)
	}
	if !result.Checked {
		t.Fatal("invalid block did not produce a validation result")
	}
	if result.Hash != wantHash {
		t.Fatalf("unexpected rejected block hash: got %x, want %x",
			result.Hash, wantHash)
	}
	if result.Mode != ValidationInvalid ||
		result.Reason != BlockResultConsensus {

		t.Fatalf("unexpected consensus rejection: %+v", result)
	}
}

func TestProcessBlockRejectsMalformedSerialization(t *testing.T) {
	kernel, err := Open(testConfig(t, Regtest))
	if err != nil {
		t.Fatalf("unable to open kernel: %v", err)
	}
	defer kernel.Close()

	_, err = kernel.ProcessBlock([]byte{0x00})
	if !errors.Is(err, ErrCreateBlock) {
		t.Fatalf("unexpected error: got %v, want %v", err, ErrCreateBlock)
	}
}

func makeRegtestBlock(t *testing.T, coinbaseValue int64) ([]byte, [32]byte) {
	t.Helper()

	coinbase := wire.NewMsgTx(1)
	coinbase.AddTxIn(wire.NewTxIn(&wire.OutPoint{
		Hash:  chainhash.Hash{},
		Index: math.MaxUint32,
	}, []byte{0x51, 0x00}, nil))
	coinbase.AddTxOut(wire.NewTxOut(coinbaseValue, []byte{0x51}))
	merkleRoot := coinbase.TxHash()

	params := &chaincfg.RegressionNetParams
	header := wire.NewBlockHeader(
		4, params.GenesisHash, &merkleRoot, params.PowLimitBits, 0,
	)
	header.Timestamp = time.Unix(time.Now().Unix(), 0)
	for {
		hash := header.BlockHash()
		if blockchain.HashToBig(&hash).Cmp(params.PowLimit) <= 0 {
			break
		}
		header.Nonce++
	}

	block := wire.NewMsgBlock(header)
	if err := block.AddTransaction(coinbase); err != nil {
		t.Fatalf("unable to add coinbase transaction: %v", err)
	}

	var serialized bytes.Buffer
	if err := block.Serialize(&serialized); err != nil {
		t.Fatalf("unable to serialize block: %v", err)
	}
	hash := block.BlockHash()
	return serialized.Bytes(), [32]byte(hash)
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
