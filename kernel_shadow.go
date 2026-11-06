// Copyright (c) 2026 The btcsuite developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"path/filepath"
	"sync"

	"github.com/btcsuite/btcd/blockchain"
	"github.com/btcsuite/btcd/btcutil/v2"
	"github.com/btcsuite/btcd/chaincfg/v2"
	"github.com/btcsuite/btcd/internal/bitcoinkernel"
	"github.com/btcsuite/btcd/wire/v2"
)

// kernelShadowValidator submits blocks to libbitcoinkernel without affecting
// the btcd acceptance path. It implements blockchain.BlockValidator.
type kernelShadowValidator struct {
	kernel *bitcoinkernel.Kernel

	mu                 sync.Mutex
	pendingTransitions []bitcoinkernel.ChainEvent
	nativeTip          *bitcoinkernel.ChainEvent
}

var _ blockchain.BlockValidator = (*kernelShadowValidator)(nil)

func newKernelShadowValidator(kernel *bitcoinkernel.Kernel) *kernelShadowValidator {
	if kernel == nil {
		return nil
	}

	return &kernelShadowValidator{kernel: kernel}
}

func (v *kernelShadowValidator) ValidateBlock(block *btcutil.Block) {
	rawBlock, err := block.Bytes()
	if err != nil {
		srvrLog.Errorf("libbitcoinkernel could not serialize block %v: %v",
			block.Hash(), err)
		return
	}

	result, err := v.kernel.ProcessBlock(rawBlock)
	v.collectCallbacks()
	if err != nil {
		srvrLog.Errorf("libbitcoinkernel could not validate block %v: %v",
			block.Hash(), err)
		return
	}
	if !result.Checked {
		// Duplicate blocks do not necessarily result in block_checked.
		srvrLog.Debugf("libbitcoinkernel shadow validator already knows block %v",
			block.Hash())
		return
	}

	switch result.Mode {
	case bitcoinkernel.ValidationValid:
		srvrLog.Debugf("libbitcoinkernel shadow validator accepted block %v",
			block.Hash())

	case bitcoinkernel.ValidationInvalid:
		if result.Reason == bitcoinkernel.BlockResultMissingPrevious {
			srvrLog.Debugf("libbitcoinkernel shadow validator deferred orphan "+
				"block %v", block.Hash())
			return
		}
		srvrLog.Warnf("libbitcoinkernel shadow validation rejected block %v: "+
			"mode=%d reason=%d", block.Hash(), result.Mode, result.Reason)

	case bitcoinkernel.ValidationInternalError:
		srvrLog.Errorf("libbitcoinkernel internal validation error for block %v: "+
			"reason=%d", block.Hash(), result.Reason)

	default:
		srvrLog.Errorf("libbitcoinkernel returned unknown validation mode for "+
			"block %v: mode=%d reason=%d", block.Hash(), result.Mode,
			result.Reason)
	}
}

// ObserveBlockChainNotification compares main-chain transitions reported by
// btcd with the corresponding libbitcoinkernel callbacks. It must be called
// from a blockchain notification callback after the btcd transition completes.
func (v *kernelShadowValidator) ObserveBlockChainNotification(
	chain *blockchain.BlockChain, notification *blockchain.Notification,
) {

	var expected bitcoinkernel.ChainEventType
	switch notification.Type {
	case blockchain.NTBlockConnected:
		expected = bitcoinkernel.BlockConnected
	case blockchain.NTBlockDisconnected:
		expected = bitcoinkernel.BlockDisconnected
	default:
		return
	}

	block, ok := notification.Data.(*btcutil.Block)
	if !ok {
		srvrLog.Errorf("unexpected blockchain notification data %T", notification.Data)
		return
	}

	v.collectCallbacks()
	hash := [32]byte(*block.Hash())
	height := block.Height()

	v.mu.Lock()
	matched := false
	for i, event := range v.pendingTransitions {
		if event.Type != expected {
			continue
		}
		if event.Hash == hash && event.Height == height {
			copy(v.pendingTransitions[i:], v.pendingTransitions[i+1:])
			v.pendingTransitions[len(v.pendingTransitions)-1] = bitcoinkernel.ChainEvent{}
			v.pendingTransitions = v.pendingTransitions[:len(v.pendingTransitions)-1]
			matched = true
			break
		}
	}
	pending := len(v.pendingTransitions)
	var nativeTip *bitcoinkernel.ChainEvent
	if v.nativeTip != nil {
		tip := *v.nativeTip
		nativeTip = &tip
	}
	v.mu.Unlock()

	if !matched {
		srvrLog.Warnf("libbitcoinkernel did not report btcd's %s block %v at height %d",
			notification.Type, block.Hash(), height)
		return
	}
	if pending != 0 || nativeTip == nil {
		// A reorganization can contain several transitions. Compare the tip
		// only after both implementations have completed that sequence.
		return
	}

	best := chain.BestSnapshot()
	if nativeTip.Hash != [32]byte(best.Hash) || nativeTip.Height != best.Height {
		srvrLog.Warnf("libbitcoinkernel tip differs from btcd after %s: "+
			"kernel=%x@%d btcd=%v@%d", notification.Type, nativeTip.Hash,
			nativeTip.Height, best.Hash, best.Height)
	}
}

func (v *kernelShadowValidator) collectCallbacks() {
	events := v.kernel.DrainChainEvents()
	errors := v.kernel.DrainErrorEvents()

	v.mu.Lock()
	for _, event := range events {
		if event.Type == bitcoinkernel.BlockTipUpdated {
			tip := event
			v.nativeTip = &tip
			continue
		}
		v.pendingTransitions = append(v.pendingTransitions, event)
	}
	v.mu.Unlock()

	for _, event := range errors {
		if event.Fatal {
			srvrLog.Errorf("libbitcoinkernel fatal error: %s", event.Message)
		} else {
			srvrLog.Errorf("libbitcoinkernel flush error: %s", event.Message)
		}
	}
}

func openConsensusKernel(params *chaincfg.Params) (*bitcoinkernel.Kernel, error) {
	if params.Net == wire.SigNet && len(cfg.SigNetChallenge) != 0 {
		return nil, fmt.Errorf("libbitcoinkernel does not support custom signet challenges")
	}

	chain, err := kernelChain(params)
	if err != nil {
		return nil, err
	}

	root := filepath.Join(cfg.DataDir, "libbitcoinkernel")
	kernel, err := bitcoinkernel.Open(bitcoinkernel.Config{
		Chain:     chain,
		DataDir:   root,
		BlocksDir: filepath.Join(root, "blocks"),
	})
	if err != nil {
		return nil, fmt.Errorf("open libbitcoinkernel: %w", err)
	}

	return kernel, nil
}

func kernelChain(params *chaincfg.Params) (bitcoinkernel.Chain, error) {
	switch params.Net {
	case wire.MainNet:
		return bitcoinkernel.Mainnet, nil
	case wire.TestNet3:
		return bitcoinkernel.Testnet, nil
	case wire.TestNet4:
		return bitcoinkernel.Testnet4, nil
	case wire.SigNet:
		return bitcoinkernel.Signet, nil
	case wire.TestNet:
		return bitcoinkernel.Regtest, nil
	default:
		return 0, fmt.Errorf("libbitcoinkernel does not support %s", params.Name)
	}
}

// bootstrapConsensusKernel brings the independent kernel chainstate up to the
// btcd block index. Blocks are replayed in parent-first order, including side
// chains, so the kernel can follow future reorganizations immediately.
func bootstrapConsensusKernel(kernel *bitcoinkernel.Kernel, chain *blockchain.BlockChain) error {
	if kernel == nil {
		return nil
	}

	// Ignore callbacks emitted while the context discovers its initial tip.
	kernel.DrainChainEvents()
	kernel.DrainErrorEvents()

	storedHashes := chain.StoredBlockHashes()
	best := chain.BestSnapshot()
	for _, hash := range storedHashes {
		block, err := chain.StoredBlockByHash(&hash)
		if err != nil {
			return fmt.Errorf("load block %v for libbitcoinkernel: %w", hash, err)
		}
		rawBlock, err := block.Bytes()
		if err != nil {
			return fmt.Errorf("serialize block %v for libbitcoinkernel: %w", hash, err)
		}
		result, err := kernel.ProcessBlock(rawBlock)
		kernel.DrainChainEvents()
		for _, event := range kernel.DrainErrorEvents() {
			return fmt.Errorf("libbitcoinkernel error while replaying %v: %s", hash,
				event.Message)
		}
		if err != nil {
			return fmt.Errorf("validate block %v with libbitcoinkernel: %w", hash, err)
		}
		if result.Checked && result.Mode != bitcoinkernel.ValidationValid &&
			chain.MainChainHasBlock(&hash) {

			return fmt.Errorf("libbitcoinkernel rejected active block %d (%v): "+
				"mode=%d reason=%d", block.Height(), block.Hash(), result.Mode,
				result.Reason)
		}
	}
	if kernelTip, ok := kernel.BestTip(); ok &&
		(kernelTip.Hash != [32]byte(best.Hash) || kernelTip.Height != best.Height) {

		return fmt.Errorf("libbitcoinkernel tip differs after replay: kernel=%x@%d btcd=%v@%d",
			kernelTip.Hash, kernelTip.Height, best.Hash, best.Height)
	}

	srvrLog.Infof("libbitcoinkernel shadow validator is ready at height %d "+
		"after replaying %d stored blocks", best.Height, len(storedHashes))
	return nil
}
