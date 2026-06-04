package decoder

import (
	"fmt"
	"testing"

	"github.com/btcsuite/btcd/fountain/distribution"
	"github.com/btcsuite/btcd/fountain/droplet"
	"github.com/btcsuite/btcd/fountain/xor"
)

type exactVerifier struct{ expected [][]byte }

func (v exactVerifier) VerifyAndLen(blockIdx uint32, candidate []byte) (int, error) {
	expected := v.expected[blockIdx]
	if len(candidate) < len(expected) {
		return 0, &VerifyError{Kind: "deserialize", Expected: "too short"}
	}
	if string(candidate[:len(expected)]) != string(expected) {
		return 0, &VerifyError{Kind: "hash_mismatch", BlockIdx: blockIdx, Expected: fmt.Sprintf("block_%d", blockIdx), Got: "mismatch"}
	}
	return len(expected), nil
}

func makeTestBlocks(k int) [][]byte {
	blocks := make([][]byte, k)
	for i := range blocks {
		size := 10 + (i%5)*3
		blocks[i] = make([]byte, size)
		for j := range blocks[i] {
			blocks[i][j] = byte((i*32 + j*17) & 0xff)
		}
	}
	return blocks
}

func TestPeelingCheckAllSingletons(t *testing.T) {
	result := PeelingCheck(3, [][]uint32{{0}, {1}, {2}})
	if !result.Success || result.Decoded != 3 {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestPeelingCheckSimplePeeling(t *testing.T) {
	result := PeelingCheck(3, [][]uint32{{0}, {0, 1}, {1, 2}})
	if !result.Success || result.Decoded != 3 {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestPeelingCheckInsufficientDroplets(t *testing.T) {
	result := PeelingCheck(3, [][]uint32{{0}})
	if result.Success || result.Decoded != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestPeelingCheckNoSingletons(t *testing.T) {
	result := PeelingCheck(3, [][]uint32{{0, 1}, {1, 2}, {0, 2}})
	if result.Success || result.Decoded != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestPeelingCheckRedundantDroplets(t *testing.T) {
	if !PeelingCheck(2, [][]uint32{{0}, {1}, {0, 1}, {0}}).Success {
		t.Fatal("expected success")
	}
}

func TestPeelingCheckChainPeeling(t *testing.T) {
	result := PeelingCheck(5, [][]uint32{{0}, {0, 1}, {1, 2}, {2, 3}, {3, 4}})
	if !result.Success || result.Decoded != 5 {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestDecodeAllSingletons(t *testing.T) {
	blocks := makeTestBlocks(3)
	droplets := make([]droplet.Droplet, len(blocks))
	for i, b := range blocks {
		droplets[i] = droplet.Droplet{DropletID: uint64(i), Indices: []uint32{uint32(i)}, PaddedLen: uint32(len(b)), Payload: append([]byte{}, b...)}
	}
	result := PeelingDecode(3, droplets, exactVerifier{blocks})
	if !result.IsSuccess() || result.DecodedCount != 3 {
		t.Fatalf("unexpected result: %+v", result)
	}
	for i, block := range result.Blocks {
		if string(*block) != string(blocks[i]) {
			t.Fatalf("block %d mismatch", i)
		}
	}
}

func TestDecodeWithXorPeeling(t *testing.T) {
	blocks := makeTestBlocks(3)
	xor01 := xor.XorBlocks([][]byte{blocks[0], blocks[1]})
	xor12 := xor.XorBlocks([][]byte{blocks[1], blocks[2]})
	droplets := []droplet.Droplet{
		{Indices: []uint32{0}, PaddedLen: uint32(len(blocks[0])), Payload: append([]byte{}, blocks[0]...)},
		{Indices: []uint32{0, 1}, PaddedLen: uint32(len(xor01)), Payload: xor01},
		{Indices: []uint32{1, 2}, PaddedLen: uint32(len(xor12)), Payload: xor12},
	}
	result := PeelingDecode(3, droplets, exactVerifier{blocks})
	if !result.IsSuccess() {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestDecodeStall(t *testing.T) {
	blocks := makeTestBlocks(3)
	droplets := []droplet.Droplet{
		{Indices: []uint32{0, 1}, PaddedLen: uint32(len(xor.XorBlocks([][]byte{blocks[0], blocks[1]}))), Payload: xor.XorBlocks([][]byte{blocks[0], blocks[1]})},
		{Indices: []uint32{1, 2}, PaddedLen: uint32(len(xor.XorBlocks([][]byte{blocks[1], blocks[2]}))), Payload: xor.XorBlocks([][]byte{blocks[1], blocks[2]})},
	}
	result := PeelingDecode(3, droplets, AcceptAllVerifier{})
	if result.IsSuccess() || result.StopReason != StopStalled || len(result.Failures) != 3 {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestDecodeWithPadding(t *testing.T) {
	blocks := [][]byte{{0xaa, 0xbb}, {0xcc, 0xdd, 0xee, 0xff}}
	padded0 := []byte{0xaa, 0xbb, 0, 0}
	xor01 := xor.XorBlocks([][]byte{padded0, blocks[1]})
	droplets := []droplet.Droplet{
		{Indices: []uint32{1}, PaddedLen: 4, Payload: append([]byte{}, blocks[1]...)},
		{Indices: []uint32{0, 1}, PaddedLen: 4, Payload: xor01},
	}
	result := PeelingDecode(2, droplets, exactVerifier{blocks})
	if !result.IsSuccess() || string(*result.Blocks[0]) != string(blocks[0]) || string(*result.Blocks[1]) != string(blocks[1]) {
		t.Fatalf("unexpected result")
	}
}

func TestDecodeFailureAnalysis(t *testing.T) {
	blocks := makeTestBlocks(5)
	droplets := []droplet.Droplet{
		{Indices: []uint32{0}, PaddedLen: uint32(len(blocks[0])), Payload: blocks[0]},
		{Indices: []uint32{1}, PaddedLen: uint32(len(blocks[1])), Payload: blocks[1]},
	}
	result := PeelingDecode(5, droplets, exactVerifier{blocks})
	if result.IsSuccess() || result.DecodedCount != 2 || len(result.Failures) != 3 {
		t.Fatalf("unexpected result: %+v", result)
	}
	for i, want := range []uint32{2, 3, 4} {
		if result.Failures[i].Index != want {
			t.Fatalf("failure %d = %d want %d", i, result.Failures[i].Index, want)
		}
	}
}

func TestRoundtripEncodeDecodeRSD(t *testing.T) {
	k := 20
	blocks := makeTestBlocks(k)
	dist := distribution.NewRobustSoliton(k, 0.1, 0.5)
	params := droplet.NewEpochParams(0, uint32(k), [32]byte{42})
	encoder := droplet.NewEncoder(&params, dist, blocks)
	result := PeelingDecode(k, encoder.GenerateN(uint64(k*5)), exactVerifier{blocks})
	if !result.IsSuccess() {
		t.Fatalf("RSD round-trip failed: decoded %d/%d", result.DecodedCount, k)
	}
}

func TestAdversarialDropletRejected(t *testing.T) {
	blocks := makeTestBlocks(3)
	corrupted := append([]byte{}, blocks[0]...)
	corrupted[0] ^= 0xff
	droplets := []droplet.Droplet{
		{Indices: []uint32{0}, PaddedLen: uint32(len(corrupted)), Payload: corrupted},
		{Indices: []uint32{0}, PaddedLen: uint32(len(blocks[0])), Payload: blocks[0]},
		{Indices: []uint32{1}, PaddedLen: uint32(len(blocks[1])), Payload: blocks[1]},
		{Indices: []uint32{2}, PaddedLen: uint32(len(blocks[2])), Payload: blocks[2]},
	}
	result := PeelingDecode(3, droplets, exactVerifier{blocks})
	if !result.IsSuccess() || result.VerifyFailures < 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
}

type rejectAllVerifier struct{}

func (rejectAllVerifier) VerifyAndLen(idx uint32, _ []byte) (int, error) {
	return 0, &VerifyError{Kind: "hash_mismatch", BlockIdx: idx, Expected: "expected", Got: "got"}
}

func TestAllVerifyFailures(t *testing.T) {
	blocks := makeTestBlocks(3)
	droplets := make([]droplet.Droplet, len(blocks))
	for i, b := range blocks {
		droplets[i] = droplet.Droplet{Indices: []uint32{uint32(i)}, PaddedLen: uint32(len(b)), Payload: b}
	}
	result := PeelingDecode(3, droplets, rejectAllVerifier{})
	if result.IsSuccess() || result.DecodedCount != 0 || result.VerifyFailures != 3 || result.StopReason != StopStalled {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestOutOfRangeDropletSkipped(t *testing.T) {
	blocks := makeTestBlocks(3)
	droplets := make([]droplet.Droplet, 0, 4)
	for i, b := range blocks {
		droplets = append(droplets, droplet.Droplet{Indices: []uint32{uint32(i)}, PaddedLen: uint32(len(b)), Payload: b})
	}
	droplets = append(droplets, droplet.Droplet{DropletID: 99, Indices: []uint32{999}, PaddedLen: 4, Payload: []byte{0, 0, 0, 0}})
	result := PeelingDecode(3, droplets, exactVerifier{blocks})
	if !result.IsSuccess() || result.DecodedCount != 3 {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestLastBlockIsNotDroppedOnCompletion(t *testing.T) {
	b0 := []byte{0xaa, 0xaa, 0xaa, 0xaa, 0xaa, 0xaa, 0xaa, 0xaa}
	b1 := []byte{0xbb, 0xbb, 0xbb, 0xbb, 0xbb, 0xbb, 0xbb, 0xbb}
	xored := xor.XorBlocks([][]byte{b0, b1})
	result := PeelingDecode(2, []droplet.Droplet{
		{Indices: []uint32{0}, PaddedLen: 8, Payload: b0},
		{Indices: []uint32{0, 1}, PaddedLen: 8, Payload: xored},
	}, AcceptAllVerifier{})
	if result.StopReason != StopCompleted || result.DecodedCount != 2 || result.Blocks[1] == nil || string(*result.Blocks[1]) != string(b1) {
		t.Fatalf("unexpected result: %+v", result)
	}
}
