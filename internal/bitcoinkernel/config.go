package bitcoinkernel

import "fmt"

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

// Config houses the options needed to create a kernel context.
//
// Chain must identify one of the networks supported by libbitcoinkernel.
type Config struct {
	Chain Chain
}

func (c Config) validate() error {
	switch c.Chain {
	case Mainnet, Testnet, Testnet4, Signet, Regtest:
		return nil
	default:
		return fmt.Errorf("%w: %s", ErrInvalidChain, c.Chain)
	}
}
