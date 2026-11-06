// Copyright (c) 2013-2026 The btcsuite developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package blockchain

import (
	"testing"

	"github.com/btcsuite/btcd/blockchain/internal/testhelper"
	"github.com/btcsuite/btcd/btcutil/v2"
	"github.com/btcsuite/btcd/chaincfg/v2"
)

type recordingBlockValidator struct {
	blocks []*btcutil.Block
}

func (v *recordingBlockValidator) ValidateBlock(block *btcutil.Block) {
	v.blocks = append(v.blocks, block)
}

func TestProcessBlockNotifiesShadowValidator(t *testing.T) {
	chain, teardown, err := chainSetup("shadowvalidator",
		&chaincfg.RegressionNetParams)
	if err != nil {
		t.Fatalf("unable to set up chain: %v", err)
	}
	defer teardown()

	validator := &recordingBlockValidator{}
	chain.blockValidator = validator

	genesis := btcutil.NewBlock(chain.chainParams.GenesisBlock)
	genesis.SetHeight(0)
	block, _, err := newBlock(chain, genesis, nil)
	if err != nil {
		t.Fatalf("unable to create block: %v", err)
	}

	isMainChain, isOrphan, err := chain.ProcessBlock(block, BFNone)
	if err != nil {
		t.Fatalf("unable to process block: %v", err)
	}
	if !isMainChain || isOrphan {
		t.Fatalf("unexpected ProcessBlock result: main=%v orphan=%v",
			isMainChain, isOrphan)
	}
	if len(validator.blocks) != 1 || validator.blocks[0] != block {
		t.Fatal("shadow validator did not receive the submitted block")
	}

	storedHashes := chain.StoredBlockHashes()
	if len(storedHashes) != 2 || storedHashes[0] != *genesis.Hash() ||
		storedHashes[1] != *block.Hash() {

		t.Fatalf("unexpected stored block order: %v", storedHashes)
	}
	storedBlock, err := chain.StoredBlockByHash(block.Hash())
	if err != nil || !storedBlock.Hash().IsEqual(block.Hash()) ||
		storedBlock.Height() != 1 {

		t.Fatalf("unable to retrieve stored block: block=%v err=%v", storedBlock, err)
	}
}

func TestProcessOrphansRevalidatesWithShadowValidator(t *testing.T) {
	chain, teardown, err := chainSetup("shadowvalidator-orphan",
		&chaincfg.RegressionNetParams)
	if err != nil {
		t.Fatalf("unable to set up chain: %v", err)
	}
	defer teardown()

	validator := &recordingBlockValidator{}
	chain.blockValidator = validator

	genesis := btcutil.NewBlock(chain.chainParams.GenesisBlock)
	genesis.SetHeight(0)
	parent, _, err := newBlock(chain, genesis, nil)
	if err != nil {
		t.Fatalf("unable to create parent block: %v", err)
	}
	child, _, err := newBlock(chain, parent, nil)
	if err != nil {
		t.Fatalf("unable to create child block: %v", err)
	}

	_, isOrphan, err := chain.ProcessBlock(child, BFNone)
	if err != nil || !isOrphan {
		t.Fatalf("unexpected orphan result: orphan=%v err=%v", isOrphan, err)
	}
	_, isOrphan, err = chain.ProcessBlock(parent, BFNone)
	if err != nil || isOrphan {
		t.Fatalf("unexpected parent result: orphan=%v err=%v", isOrphan, err)
	}

	if len(validator.blocks) != 3 || validator.blocks[0] != child ||
		validator.blocks[1] != parent || validator.blocks[2] != child {

		t.Fatalf("unexpected shadow validation order: %+v", validator.blocks)
	}
}

// TestMaybeAcceptBlockReusesHeaderNode ensures that when a block header is
// processed first via ProcessBlockHeader and later the full block arrives via
// ProcessBlock, the existing blockNode pointer is reused rather than replaced.
// Replacing the pointer would orphan the entry held by bestHeader's chainView,
// causing bestHeader.Contains(index.LookupNode(hash)) to return false and
// breaking IsValidHeader and downstream netsync checks.
func TestMaybeAcceptBlockReusesHeaderNode(t *testing.T) {
	chain, params, tearDown := utxoCacheTestChain(
		"TestMaybeAcceptBlockReusesHeaderNode")
	defer tearDown()

	// Build a base chain of 3 blocks.
	//
	// genesis -> 1 -> 2 -> 3
	tip := btcutil.NewBlock(params.GenesisBlock)
	// Generated descendants must start at height 1 so the coinbase height
	// encoding matches regtest's BIP34 rules.
	tip.SetHeight(0)
	_, _, err := addBlocks(3, chain, tip, []*testhelper.SpendableOut{})
	if err != nil {
		t.Fatalf("failed to build base chain: %v", err)
	}

	// Create block 4 without processing it.
	prevBlock, err := chain.BlockByHeight(3)
	if err != nil {
		t.Fatalf("failed to get block at height 3: %v", err)
	}
	block4, _, err := newBlock(chain, prevBlock, nil)
	if err != nil {
		t.Fatalf("failed to create block 4: %v", err)
	}

	// Process block 4's header first.
	block4Hash := block4.Hash()
	_, err = chain.ProcessBlockHeader(
		&block4.MsgBlock().Header, BFNone, false)
	if err != nil {
		t.Fatalf("ProcessBlockHeader fail: %v", err)
	}

	// Capture the header-only node pointer from the index.
	headerNode := chain.index.LookupNode(block4Hash)
	if headerNode == nil {
		t.Fatal("header node not found in block index")
	}

	// Now process the full block.
	_, _, err = chain.ProcessBlock(block4, BFNone)
	if err != nil {
		t.Fatalf("ProcessBlock fail: %v", err)
	}

	// The index must still hold the same pointer that bestHeader has.
	// Before the fix, maybeAcceptBlock would create a fresh node and
	// overwrite the index entry, orphaning the pointer in bestHeader.
	fullBlockNode := chain.index.LookupNode(block4Hash)
	if fullBlockNode != headerNode {
		t.Fatal("ProcessBlock replaced the header node pointer " +
			"instead of reusing it")
	}
	if !chain.bestHeader.Contains(fullBlockNode) {
		t.Fatal("node no longer in bestHeader after ProcessBlock")
	}
}
