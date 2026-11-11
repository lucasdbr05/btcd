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
	chain              *blockchain.BlockChain
	pendingTransitions []bitcoinkernel.ChainEvent
	nativeTip          *bitcoinkernel.ChainEvent
	nativeResults      map[[32]byte]nativeBlockResult
}

var _ blockchain.BlockValidator = (*kernelShadowValidator)(nil)
var _ blockchain.BlockValidationResultObserver = (*kernelShadowValidator)(nil)

// nativeBlockResult is the result emitted by libbitcoinkernel for one
// ProcessBlock submission.  The kernel callback uses a different result-code
// space than btcd, so both values are preserved for diagnostics.
type nativeBlockResult struct {
	result bitcoinkernel.BlockResult
	err    error
}

func newKernelShadowValidator(kernel *bitcoinkernel.Kernel) *kernelShadowValidator {
	if kernel == nil {
		return nil
	}

	return &kernelShadowValidator{
		kernel:        kernel,
		nativeResults: make(map[[32]byte]nativeBlockResult),
	}
}

// SetChain supplies the btcd chain used to add best-tip context to shadow
// validation diagnostics. It is called once during server initialization.
func (v *kernelShadowValidator) SetChain(chain *blockchain.BlockChain) {
	if v == nil {
		return
	}

	v.mu.Lock()
	v.chain = chain
	v.mu.Unlock()
}

func (v *kernelShadowValidator) ValidateBlock(block *btcutil.Block) {
	// A btcd ProcessBlock call synchronously emits all of its main-chain
	// notifications before the next block reaches this observer. Anything
	// remaining here therefore denotes a kernel-only transition.
	v.collectCallbacks()
	v.reportPendingTransitions(block)

	rawBlock, err := block.Bytes()
	if err != nil {
		srvrLog.Errorf("libbitcoinkernel could not serialize block %v: %v",
			block.Hash(), err)
		return
	}

	result, err := v.kernel.ProcessBlock(rawBlock)
	v.collectCallbacks()
	v.storeNativeResult([32]byte(*block.Hash()), nativeBlockResult{
		result: result,
		err:    err,
	})
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

// ObserveBlockValidationResult compares btcd's final disposition for a block
// with the corresponding libbitcoinkernel result.  It is called after btcd
// has released the chain lock and never changes btcd's acceptance decision.
func (v *kernelShadowValidator) ObserveBlockValidationResult(
	block *btcutil.Block, btcdResult blockchain.BlockValidationResult,
) {
	v.collectCallbacks()

	hash := [32]byte(*block.Hash())
	native, ok := v.takeNativeResult(hash)
	if !ok {
		srvrLog.Debugf("libbitcoinkernel did not report a validation result for "+
			"btcd's %s block %v", btcdResult.Outcome, block.Hash())
		return
	}
	if native.err != nil {
		srvrLog.Errorf("libbitcoinkernel could not compare btcd's %s block %v: %v",
			btcdResult.Outcome, block.Hash(), native.err)
		return
	}

	if v.resultsAgree(btcdResult, native.result) {
		srvrLog.Debugf("libbitcoinkernel agrees with btcd for block %v: "+
			"btcd=%s kernel-mode=%d kernel-reason=%d", block.Hash(),
			btcdResult.Outcome, native.result.Mode, native.result.Reason)
		return
	}

	btcdReason := ""
	btcdCode := ""
	if btcdResult.RuleError != nil {
		btcdCode = btcdResult.RuleError.ErrorCode.String()
		btcdReason = btcdResult.RuleError.Description
	} else if btcdResult.ProcessError != nil {
		btcdReason = btcdResult.ProcessError.Error()
	}
	v.logDivergence("block-validation", block.Hash().String(), block.Height(),
		"btcd_outcome=%s btcd_code=%q btcd_reason=%q kernel_checked=%t "+
			"kernel_new_block=%t kernel_mode=%d kernel_reason=%d",
		btcdResult.Outcome, btcdCode, btcdReason, native.result.Checked,
		native.result.NewBlock, native.result.Mode, native.result.Reason)
}

func (v *kernelShadowValidator) storeNativeResult(hash [32]byte, result nativeBlockResult) {
	v.mu.Lock()
	defer v.mu.Unlock()

	v.nativeResults[hash] = result
}

func (v *kernelShadowValidator) takeNativeResult(hash [32]byte) (nativeBlockResult, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()

	result, ok := v.nativeResults[hash]
	if ok {
		delete(v.nativeResults, hash)
	}
	return result, ok
}

func (v *kernelShadowValidator) resultsAgree(btcdResult blockchain.BlockValidationResult,
	nativeResult bitcoinkernel.BlockResult) bool {

	switch btcdResult.Outcome {
	case blockchain.BlockAcceptedMainChain, blockchain.BlockAcceptedSideChain:
		return nativeResult.Checked && nativeResult.Mode == bitcoinkernel.ValidationValid

	case blockchain.BlockOrphaned:
		return nativeResult.Checked && nativeResult.Mode == bitcoinkernel.ValidationInvalid &&
			nativeResult.Reason == bitcoinkernel.BlockResultMissingPrevious

	case blockchain.BlockDuplicate:
		// Neither implementation is required to emit a result for a block it
		// already knows, so a missing block_checked callback is expected.
		return !nativeResult.Checked

	case blockchain.BlockRejected:
		return nativeResult.Checked && nativeResult.Mode == bitcoinkernel.ValidationInvalid

	case blockchain.BlockProcessingError:
		return false

	default:
		return false
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
	expectedEvent := bitcoinkernel.ChainEvent{
		Type:   expected,
		Hash:   [32]byte(*block.Hash()),
		Height: block.Height(),
	}
	nativeEvent, matched, pending := v.consumeTransition(expectedEvent)
	if !matched {
		if nativeEvent == nil {
			v.logDivergence("missing-kernel-transition", block.Hash().String(),
				block.Height(), "btcd_transition=%s", notification.Type)
		} else {
			v.logDivergence("transition-order", block.Hash().String(), block.Height(),
				"btcd_transition=%s kernel_transition=%d kernel_hash=%x "+
					"kernel_height=%d", notification.Type, nativeEvent.Type,
				nativeEvent.Hash, nativeEvent.Height)
		}
		return
	}
	if pending != 0 {
		// A reorganization can contain several transitions. Compare the tip
		// only after both implementations have completed that sequence.
		return
	}

	nativeTip := v.bestNativeTip()
	if nativeTip == nil {
		v.logDivergence("missing-kernel-tip", block.Hash().String(), block.Height(),
			"btcd_transition=%s", notification.Type)
		return
	}
	best := chain.BestSnapshot()
	if nativeTip.Hash != [32]byte(best.Hash) || nativeTip.Height != best.Height {
		v.logDivergence("chain-tip", block.Hash().String(), block.Height(),
			"btcd_transition=%s kernel_tip_hash=%x kernel_tip_height=%d "+
				"btcd_tip_hash=%v btcd_tip_height=%d", notification.Type,
			nativeTip.Hash, nativeTip.Height, best.Hash, best.Height)
	}
}

// consumeTransition removes the next kernel transition and verifies that it is
// exactly the transition btcd reported. A strict FIFO comparison detects both
// missing and reordered steps in reorganizations.
func (v *kernelShadowValidator) consumeTransition(expected bitcoinkernel.ChainEvent) (
	*bitcoinkernel.ChainEvent, bool, int,
) {
	v.mu.Lock()
	defer v.mu.Unlock()

	if len(v.pendingTransitions) == 0 {
		return nil, false, 0
	}

	actual := v.pendingTransitions[0]
	v.pendingTransitions[0] = bitcoinkernel.ChainEvent{}
	v.pendingTransitions = v.pendingTransitions[1:]
	return &actual, actual == expected, len(v.pendingTransitions)
}

func (v *kernelShadowValidator) bestNativeTip() *bitcoinkernel.ChainEvent {
	v.mu.Lock()
	defer v.mu.Unlock()

	if v.nativeTip == nil {
		return nil
	}
	tip := *v.nativeTip
	return &tip
}

// reportPendingTransitions clears and reports native transitions which btcd
// did not report before the next block began processing. Clearing them lets a
// later, independent sequence be compared without a cascading false match.
func (v *kernelShadowValidator) reportPendingTransitions(block *btcutil.Block) {
	v.mu.Lock()
	pending := v.pendingTransitions
	v.pendingTransitions = nil
	v.mu.Unlock()

	for _, event := range pending {
		v.logDivergence("kernel-only-transition", block.Hash().String(),
			block.Height(), "kernel_transition=%d kernel_hash=%x kernel_height=%d",
			event.Type, event.Hash, event.Height)
	}
}

func (v *kernelShadowValidator) logDivergence(kind, blockHash string,
	blockHeight int32, format string, args ...interface{}) {

	chainTipHash := ""
	chainTipHeight := int32(-1)
	v.mu.Lock()
	chain := v.chain
	v.mu.Unlock()
	if chain != nil {
		best := chain.BestSnapshot()
		chainTipHash = best.Hash.String()
		chainTipHeight = best.Height
	}

	detail := fmt.Sprintf(format, args...)
	srvrLog.Warnf("libbitcoinkernel divergence kind=%s block_hash=%s "+
		"block_height=%d chain_tip_hash=%s chain_tip_height=%d %s", kind,
		blockHash, blockHeight, chainTipHash, chainTipHeight, detail)
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
