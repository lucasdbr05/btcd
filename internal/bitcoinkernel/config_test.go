package bitcoinkernel

import (
	"errors"
	"testing"
)

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

	kernel, err := Open(Config{Chain: Chain(255)})
	if !errors.Is(err, ErrInvalidChain) {
		t.Fatalf("unexpected error: got %v, want %v", err,
			ErrInvalidChain)
	}
	if kernel != nil {
		t.Fatal("invalid configuration returned a kernel")
	}
}

func TestNilKernelClose(t *testing.T) {
	t.Parallel()

	var kernel *Kernel
	if err := kernel.Close(); err != nil {
		t.Fatalf("unable to close a nil kernel: %v", err)
	}
}
