package superblock

import (
	"bytes"
	"math"
	"reflect"
	"testing"

	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/fountain/distribution"
	"github.com/btcsuite/btcd/fountain/droplet"
	"github.com/btcsuite/btcd/fountain/xor"
	"github.com/btcsuite/btcd/wire"
)

func makeFakeBlocks(sizes []int) [][]byte {
	blocks := make([][]byte, len(sizes))
	for i, sz := range sizes {
		blocks[i] = make([]byte, sz)
		for j := range blocks[i] {
			blocks[i][j] = byte((i * 31) & 0xff)
		}
	}
	return blocks
}

func makeWireBlock(t *testing.T, seed byte) []byte {
	t.Helper()

	coinbase := wire.NewMsgTx(wire.TxVersion)
	coinbase.AddTxIn(&wire.TxIn{
		PreviousOutPoint: wire.OutPoint{Index: math.MaxUint32},
		SignatureScript:  []byte{seed},
		Sequence:         wire.MaxTxInSequenceNum,
	})
	coinbase.AddTxOut(&wire.TxOut{Value: int64(seed) * 1000, PkScript: []byte{0x51}})

	var txHash chainhash.Hash
	txHash[0] = seed

	hdr := wire.NewBlockHeader(1, &chainhash.Hash{}, &txHash, 0x1d00ffff, 0)
	block := wire.NewMsgBlock(hdr)
	if err := block.AddTransaction(coinbase); err != nil {
		t.Fatalf("AddTransaction: %v", err)
	}

	var buf bytes.Buffer
	if err := block.Serialize(&buf); err != nil {
		t.Fatalf("Serialize block: %v", err)
	}
	return buf.Bytes()
}

func TestGroupingByTargetSize(t *testing.T) {
	blocks := makeFakeBlocks([]int{100, 200, 300, 400})
	supers, counts := BlocksToSuperblocks(blocks, 350)

	if len(supers) != 3 {
		t.Fatalf("want 3 superblocks, got %d", len(supers))
	}
	wantCounts := []int{2, 1, 1}
	if !reflect.DeepEqual(counts, wantCounts) {
		t.Fatalf("want counts %v, got %v", wantCounts, counts)
	}
	if len(supers[0]) != 300 { // 100+200
		t.Fatalf("super[0] len want 300, got %d", len(supers[0]))
	}
	if len(supers[1]) != 300 {
		t.Fatalf("super[1] len want 300, got %d", len(supers[1]))
	}
	if len(supers[2]) != 400 {
		t.Fatalf("super[2] len want 400, got %d", len(supers[2]))
	}
}

func TestAllFitInOne(t *testing.T) {
	blocks := makeFakeBlocks([]int{100, 200, 300})
	supers, counts := BlocksToSuperblocks(blocks, 10_000)

	if len(supers) != 1 {
		t.Fatalf("want 1 superblock, got %d", len(supers))
	}
	if counts[0] != 3 {
		t.Fatalf("want count 3, got %d", counts[0])
	}
	if len(supers[0]) != 600 {
		t.Fatalf("super[0] len want 600, got %d", len(supers[0]))
	}
}

func TestOversizedBlockBecomesSingleton(t *testing.T) {
	blocks := makeFakeBlocks([]int{100, 5000, 200})
	supers, counts := BlocksToSuperblocks(blocks, 1000)

	if len(supers) != 3 {
		t.Fatalf("want 3 superblocks, got %d", len(supers))
	}
	if counts[1] != 1 {
		t.Fatalf("oversized block want count 1, got %d", counts[1])
	}
	if len(supers[1]) != 5000 {
		t.Fatalf("super[1] len want 5000, got %d", len(supers[1]))
	}
}

func TestEachBlockOwnSuperblockWhenTargetTiny(t *testing.T) {
	blocks := makeFakeBlocks([]int{100, 200})
	supers, counts := BlocksToSuperblocks(blocks, 1)

	if len(supers) != 2 {
		t.Fatalf("want 2 superblocks, got %d", len(supers))
	}
	if !reflect.DeepEqual(counts, []int{1, 1}) {
		t.Fatalf("want counts [1,1], got %v", counts)
	}
	if !bytes.Equal(supers[0], blocks[0]) || !bytes.Equal(supers[1], blocks[1]) {
		t.Fatal("superblock content mismatch")
	}
}

func TestEmptyInputProducesEmptyOutput(t *testing.T) {
	supers, counts := BlocksToSuperblocks([][]byte{}, 5000)
	if supers != nil || counts != nil {
		t.Fatalf("want nil, got supers=%v counts=%v", supers, counts)
	}
}

func TestZeroTargetPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for targetBytes=0")
		}
	}()
	BlocksToSuperblocks(makeFakeBlocks([]int{100}), 0)
}

func TestSuperblockContentIsConcat(t *testing.T) {
	a := []byte{0xAA, 0xAB, 0xAC}
	b := []byte{0xBB, 0xBC}
	supers, counts := BlocksToSuperblocks([][]byte{a, b}, 10)

	if len(supers) != 1 {
		t.Fatalf("expected 1 superblock")
	}
	if counts[0] != 2 {
		t.Fatalf("expected count 2")
	}
	want := append(append([]byte{}, a...), b...)
	if !bytes.Equal(supers[0], want) {
		t.Fatalf("content mismatch: want %v, got %v", want, supers[0])
	}
}

func TestSuperblocksToBlocksRoundtrip(t *testing.T) {
	rawBlocks := [][]byte{
		makeWireBlock(t, 1),
		makeWireBlock(t, 2),
		makeWireBlock(t, 3),
	}
	supers, counts := BlocksToSuperblocks(rawBlocks, 1_000_000)

	wrapped := make([]*[]byte, len(supers))
	for i := range supers {
		cp := supers[i]
		wrapped[i] = &cp
	}

	recovered := SuperblocksToBlocks(wrapped, counts, len(rawBlocks))

	if len(recovered) != len(rawBlocks) {
		t.Fatalf("want %d blocks, got %d", len(rawBlocks), len(recovered))
	}
	for i, got := range recovered {
		if got == nil {
			t.Fatalf("block %d is nil", i)
		}
		if !bytes.Equal(*got, rawBlocks[i]) {
			t.Fatalf("block %d content mismatch", i)
		}
	}
}

func TestSuperblocksToBlocksMultipleSuperblocks(t *testing.T) {
	rawBlocks := [][]byte{
		makeWireBlock(t, 10),
		makeWireBlock(t, 20),
		makeWireBlock(t, 30),
		makeWireBlock(t, 40),
	}

	supers, counts := BlocksToSuperblocks(rawBlocks, 1)

	wrapped := make([]*[]byte, len(supers))
	for i := range supers {
		cp := supers[i]
		wrapped[i] = &cp
	}

	recovered := SuperblocksToBlocks(wrapped, counts, len(rawBlocks))
	for i, got := range recovered {
		if got == nil {
			t.Fatalf("block %d is nil", i)
		}
		if !bytes.Equal(*got, rawBlocks[i]) {
			t.Fatalf("block %d mismatch", i)
		}
	}
}

func TestSuperblocksToBlocksMissSuperblock(t *testing.T) {
	rawBlocks := [][]byte{
		makeWireBlock(t, 1),
		makeWireBlock(t, 2),
		makeWireBlock(t, 3),
	}
	supers, counts := BlocksToSuperblocks(rawBlocks, 1_000_000)
	_ = supers

	wrapped := []*[]byte{nil}

	recovered := SuperblocksToBlocks(wrapped, counts, len(rawBlocks))
	for i, got := range recovered {
		if got != nil {
			t.Fatalf("block %d should be nil (superblock not recovered), got non-nil", i)
		}
	}
}

func TestManifestSerializationRoundtrip(t *testing.T) {
	epochs := []EpochManifest{
		{EpochID: 0, Manifest: Manifest{TotalBlocks: 95, TotalSupers: 3, BlockCounts: []int{40, 30, 25}}},
		{EpochID: 1, Manifest: Manifest{TotalBlocks: 50, TotalSupers: 2, BlockCounts: []int{25, 25}}},
	}
	data := SerializeManifests(epochs)
	got, err := DeserializeManifests(data)
	if err != nil {
		t.Fatalf("DeserializeManifests: %v", err)
	}
	if !reflect.DeepEqual(epochs, got) {
		t.Fatalf("roundtrip mismatch:\nwant %+v\ngot  %+v", epochs, got)
	}
}

func TestManifestSingleEpoch(t *testing.T) {
	epochs := []EpochManifest{
		{EpochID: 42, Manifest: Manifest{TotalBlocks: 10, TotalSupers: 1, BlockCounts: []int{10}}},
	}
	data := SerializeManifests(epochs)
	got, err := DeserializeManifests(data)
	if err != nil {
		t.Fatalf("DeserializeManifests: %v", err)
	}
	if !reflect.DeepEqual(epochs, got) {
		t.Fatalf("roundtrip mismatch")
	}
}

func TestManifestEmpty(t *testing.T) {
	data := SerializeManifests(nil)
	got, err := DeserializeManifests(data)
	if err != nil {
		t.Fatalf("DeserializeManifests: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("want empty, got %v", got)
	}
}

func TestManifestCorrupted(t *testing.T) {
	_, err := DeserializeManifests([]byte{0xfe, 0x00}) // truncated
	if err == nil {
		t.Fatal("expected error for corrupted data")
	}
}

func peelDecode(k int, droplets []droplet.Droplet) []*[]byte {
	n := len(droplets)
	payloads := make([][]byte, n)
	degree := make([]int, n)
	xorIdx := make([]uint32, n)
	blockToDroplets := make([][]int, k)

	for i, d := range droplets {
		if len(d.Indices) == 0 {
			continue
		}
		var xi uint32
		for _, idx := range d.Indices {
			if int(idx) >= k {
				continue
			}
			xi ^= idx
			blockToDroplets[idx] = append(blockToDroplets[idx], i)
		}
		degree[i] = len(d.Indices)
		xorIdx[i] = xi
		payloads[i] = append([]byte{}, d.Payload...)
	}

	blocks := make([]*[]byte, k)
	decoded := 0

	changed := true
	for changed && decoded < k {
		changed = false
		for di := range n {
			if degree[di] != 1 {
				continue
			}
			bi := xorIdx[di]
			if int(bi) >= k || blocks[bi] != nil {
				continue
			}
			b := append([]byte(nil), payloads[di][:droplets[di].PaddedLen]...)
			blocks[bi] = &b
			decoded++
			changed = true

			for _, ref := range blockToDroplets[bi] {
				if ref == di {
					continue
				}
				xor.XorIntoFixed(payloads[ref], *blocks[bi])
				degree[ref]--
				xorIdx[ref] ^= bi
			}
			blockToDroplets[bi] = nil
		}
	}
	return blocks
}

func TestFullRoundtripWithSuperblocks(t *testing.T) {
	rawBlocks := make([][]byte, 6)
	for i := range rawBlocks {
		rawBlocks[i] = makeWireBlock(t, byte(i+1))
	}

	targetSize := len(rawBlocks[0]) + len(rawBlocks[1]) + 1
	supers, blockCounts := BlocksToSuperblocks(rawBlocks, targetSize)
	k := len(supers)

	if k < 2 {
		t.Skip("fewer than 2 superblocks; adjust targetSize")
	}

	dist := distribution.NewRobustSoliton(k, 0.1, 0.5)
	params := droplet.NewEpochParams(0, uint32(k), [32]byte{7})
	enc := droplet.NewEncoder(&params, dist, supers)

	drops := enc.GenerateN(uint64(k * 6))

	decodedSupers := peelDecode(k, drops)

	for i, sb := range decodedSupers {
		if sb == nil {
			t.Fatalf("superblock %d not recovered", i)
		}
	}

	totalBlocks := len(rawBlocks)
	recovered := SuperblocksToBlocks(decodedSupers, blockCounts, totalBlocks)

	for i, got := range recovered {
		if got == nil {
			t.Fatalf("block %d not recovered after reassembly", i)
		}
		if !bytes.Equal(*got, rawBlocks[i]) {
			t.Fatalf("block %d content mismatch", i)
		}
	}
}
