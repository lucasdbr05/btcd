package bitcoinkernel

import (
	"errors"
	"path/filepath"
	"testing"
)

func testConfig(t *testing.T, chain Chain) Config {
	t.Helper()

	rootDir := t.TempDir()
	return Config{
		Chain:     chain,
		DataDir:   filepath.Join(rootDir, "data"),
		BlocksDir: filepath.Join(rootDir, "blocks"),
	}
}

func TestChainString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		chain Chain
		want  string
	}{
		{chain: Mainnet, want: "mainnet"},
		{chain: Testnet, want: "testnet"},
		{chain: Testnet4, want: "testnet4"},
		{chain: Signet, want: "signet"},
		{chain: Regtest, want: "regtest"},
		{chain: Chain(255), want: "unknown(255)"},
	}

	for _, test := range tests {
		test := test
		t.Run(test.want, func(t *testing.T) {
			t.Parallel()

			if got := test.chain.String(); got != test.want {
				t.Fatalf("unexpected chain name: got %q, want %q", got,
					test.want)
			}
		})
	}
}

func TestOpenRejectsInvalidChain(t *testing.T) {
	t.Parallel()

	cfg := testConfig(t, Chain(255))
	kernel, err := Open(cfg)
	if !errors.Is(err, ErrInvalidChain) {
		t.Fatalf("unexpected error: got %v, want %v", err,
			ErrInvalidChain)
	}
	if kernel != nil {
		t.Fatal("invalid configuration returned a kernel")
	}
}

func TestConfigValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		update func(*Config)
		want   error
	}{
		{
			name: "missing data directory",
			update: func(cfg *Config) {
				cfg.DataDir = ""
			},
			want: ErrInvalidDataDir,
		},
		{
			name: "relative blocks directory",
			update: func(cfg *Config) {
				cfg.BlocksDir = "blocks"
			},
			want: ErrInvalidBlocksDir,
		},
		{
			name: "too many workers",
			update: func(cfg *Config) {
				cfg.WorkerThreads = 16
			},
			want: ErrInvalidWorkerThreads,
		},
		{
			name: "cache below minimum",
			update: func(cfg *Config) {
				cfg.DBCacheBytes = minDatabaseCacheBytes - 1
			},
			want: ErrInvalidDBCache,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			cfg := testConfig(t, Regtest)
			test.update(&cfg)
			if err := cfg.validate(); !errors.Is(err, test.want) {
				t.Fatalf("unexpected error: got %v, want %v", err,
					test.want)
			}
		})
	}
}

func TestNilKernelClose(t *testing.T) {
	t.Parallel()

	var kernel *Kernel
	if err := kernel.Close(); err != nil {
		t.Fatalf("unable to close a nil kernel: %v", err)
	}
}

func TestProcessBlockInputValidation(t *testing.T) {
	t.Parallel()

	var kernel *Kernel
	if _, err := kernel.ProcessBlock([]byte{0x00}); !errors.Is(err, ErrClosed) {
		t.Fatalf("unexpected nil kernel error: got %v, want %v", err,
			ErrClosed)
	}

	kernel = &Kernel{}
	if _, err := kernel.ProcessBlock(nil); !errors.Is(err, ErrEmptyBlock) {
		t.Fatalf("unexpected empty block error: got %v, want %v", err,
			ErrEmptyBlock)
	}
	if _, err := kernel.ProcessBlock([]byte{0x00}); !errors.Is(err, ErrClosed) {
		t.Fatalf("unexpected closed kernel error: got %v, want %v", err,
			ErrClosed)
	}
}
