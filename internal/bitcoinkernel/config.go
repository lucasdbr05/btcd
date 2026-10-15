package bitcoinkernel

import (
	"fmt"
	"path/filepath"
	"strings"
)

const minDatabaseCacheBytes = 4 * 1024 * 1024

// Chain identifies a Bitcoin network supported by libbitcoinkernel.
type Chain uint8

// Supported Bitcoin networks.
const (
	Mainnet Chain = iota
	Testnet
	Testnet4
	Signet
	Regtest
)

// String returns the human-readable name of the chain.
func (c Chain) String() string {
	switch c {
	case Mainnet:
		return "mainnet"
	case Testnet:
		return "testnet"
	case Testnet4:
		return "testnet4"
	case Signet:
		return "signet"
	case Regtest:
		return "regtest"
	default:
		return fmt.Sprintf("unknown(%d)", uint8(c))
	}
}

// Config houses the options needed to create a kernel context and chainstate
// manager.
//
// Chain must identify one of the networks supported by libbitcoinkernel.
type Config struct {
	Chain Chain

	// DataDir is the directory containing the kernel chainstate data.
	DataDir string

	// BlocksDir is the directory containing the kernel block data.
	BlocksDir string

	// WorkerThreads controls the validation worker pool.  Valid values are
	// between zero and 15, inclusive.
	WorkerThreads int

	// DBCacheBytes controls the total database cache.  A value of zero uses
	// the kernel default.  Non-zero values must be at least 4 MiB.
	DBCacheBytes uint64
}

func (c Config) validate() error {
	switch c.Chain {
	case Mainnet, Testnet, Testnet4, Signet, Regtest:
	default:
		return fmt.Errorf("%w: %s", ErrInvalidChain, c.Chain)
	}

	if err := validateDirectory(c.DataDir, ErrInvalidDataDir); err != nil {
		return err
	}
	if err := validateDirectory(c.BlocksDir, ErrInvalidBlocksDir); err != nil {
		return err
	}
	if c.WorkerThreads < 0 || c.WorkerThreads > 15 {
		return fmt.Errorf("%w: %d", ErrInvalidWorkerThreads,
			c.WorkerThreads)
	}
	if c.DBCacheBytes != 0 && c.DBCacheBytes < minDatabaseCacheBytes {
		return fmt.Errorf("%w: %d", ErrInvalidDBCache, c.DBCacheBytes)
	}

	return nil
}

func validateDirectory(path string, target error) error {
	if path == "" || !filepath.IsAbs(path) || strings.IndexByte(path, 0) >= 0 {
		return fmt.Errorf("%w: %q", target, path)
	}

	return nil
}
