package decoder

import (
	"bytes"
	"fmt"

	"github.com/btcsuite/btcd/blockchain"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/wire"
)

// BitcoinBlockVerifier implements BlockVerifier for raw Bitcoin wire-format blocks.
// It verifies the recovered bytes against the trusted header chain by checking:
//  1. The bytes deserialize as a valid wire.MsgBlock.
//  2. The block hash matches the trusted header at blockIdx.
//  3. The computed Merkle root matches the header's MerkleRoot field.
type BitcoinBlockVerifier struct {
	Headers []wire.BlockHeader
}

// SuperblockBitcoinVerifier verifies superblocks composed of concatenated Bitcoin blocks.
// Each superblock contains blockCounts[i] consecutive blocks; each constituent block is
// verified individually against its trusted header.
type SuperblockBitcoinVerifier struct {
	Headers     []wire.BlockHeader
	BlockCounts []int
	offsets     []int
}

func NewBitcoinBlockVerifier(headers []wire.BlockHeader) *BitcoinBlockVerifier {
	return &BitcoinBlockVerifier{Headers: headers}
}

func (v *BitcoinBlockVerifier) VerifyAndLen(blockIdx uint32, candidate []byte) (int, error) {
	var msg wire.MsgBlock
	if err := msg.Deserialize(bytes.NewReader(candidate)); err != nil {
		return 0, &VerifyError{Kind: "deserialize", Expected: err.Error()}
	}

	var buf bytes.Buffer
	if err := msg.Serialize(&buf); err != nil {
		return 0, &VerifyError{Kind: "deserialize", Expected: err.Error()}
	}
	trueLen := buf.Len()

	if v.Headers == nil || int(blockIdx) >= len(v.Headers) {
		return trueLen, nil
	}

	trusted := v.Headers[blockIdx]

	// 1- Block hash check.
	got := msg.BlockHash()
	expected := trusted.BlockHash()
	if got != expected {
		return 0, &VerifyError{
			Kind:     "hash_mismatch",
			BlockIdx: blockIdx,
			Expected: expected.String(),
			Got:      got.String(),
		}
	}

	// 2- Merkle root check.
	block := btcutil.NewBlock(&msg)
	merkles := blockchain.BuildMerkleTreeStore(block.Transactions(), false)
	if len(merkles) == 0 {
		return 0, &VerifyError{Kind: "deserialize", Expected: "empty merkle tree"}
	}
	computedRoot := merkles[len(merkles)-1]
	if computedRoot == nil || *computedRoot != trusted.MerkleRoot {
		got := chainhash.Hash{}
		if computedRoot != nil {
			got = *computedRoot
		}
		return 0, &VerifyError{
			Kind:     "merkle_mismatch",
			BlockIdx: blockIdx,
			Expected: trusted.MerkleRoot.String(),
			Got:      got.String(),
		}
	}

	return trueLen, nil
}

func NewSuperblockBitcoinVerifier(headers []wire.BlockHeader, blockCounts []int) *SuperblockBitcoinVerifier {
	offsets := make([]int, len(blockCounts))
	cur := 0
	for i, c := range blockCounts {
		offsets[i] = cur
		cur += c
	}
	return &SuperblockBitcoinVerifier{Headers: headers, BlockCounts: blockCounts, offsets: offsets}
}

func (v *SuperblockBitcoinVerifier) VerifyAndLen(superIdx uint32, candidate []byte) (int, error) {
	if int(superIdx) >= len(v.BlockCounts) {
		return 0, fmt.Errorf("superblock index %d out of range", superIdx)
	}
	count := v.BlockCounts[superIdx]
	startBlock := v.offsets[superIdx]

	r := bytes.NewReader(candidate)
	total := 0
	for i := range count {
		var msg wire.MsgBlock
		before := r.Len()
		if err := msg.Deserialize(r); err != nil {
			return 0, &VerifyError{Kind: "deserialize", Expected: err.Error()}
		}
		consumed := before - r.Len()
		total += consumed

		blockIdx := uint32(startBlock + i)
		if v.Headers == nil || int(blockIdx) >= len(v.Headers) {
			continue
		}
		trusted := v.Headers[blockIdx]
		got := msg.BlockHash()
		expected := trusted.BlockHash()
		if got != expected {
			return 0, &VerifyError{
				Kind:     "hash_mismatch",
				BlockIdx: blockIdx,
				Expected: expected.String(),
				Got:      got.String(),
			}
		}
		block := btcutil.NewBlock(&msg)
		merkles := blockchain.BuildMerkleTreeStore(block.Transactions(), false)
		if len(merkles) > 0 && merkles[len(merkles)-1] != nil {
			if *merkles[len(merkles)-1] != trusted.MerkleRoot {
				return 0, &VerifyError{
					Kind:     "merkle_mismatch",
					BlockIdx: blockIdx,
					Expected: trusted.MerkleRoot.String(),
					Got:      merkles[len(merkles)-1].String(),
				}
			}
		}
	}
	return total, nil
}
