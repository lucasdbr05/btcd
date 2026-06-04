package droplet

import (
	"math/rand/v2"
	"testing"

	"github.com/btcsuite/btcd/fountain/distribution"
	"github.com/btcsuite/btcd/fountain/xor"
)

type fixedDegree int

func (d fixedDegree) SampleDegree(_ *rand.Rand) int { return int(d) }
func (d fixedDegree) ExpectedDegree() float64       { return float64(d) }

func testBlocks(k int) [][]byte {
	blocks := make([][]byte, k)
	for i := range blocks {
		size := 100 + (i%50)*10
		blocks[i] = make([]byte, size)
		for j := range blocks[i] {
			blocks[i][j] = byte((i*31 + j*17) & 0xff)
		}
	}
	return blocks
}

func TestGenerateSingleton(t *testing.T) {
	k := 50
	blocks := testBlocks(k)
	params := NewEpochParams(0, uint32(k), [32]byte{42})
	encoder := NewEncoder(&params, fixedDegree(1), blocks)
	d := encoder.Generate(0)
	if len(d.Indices) != 1 {
		t.Fatalf("degree = %d, want 1", len(d.Indices))
	}
	idx := d.Indices[0]
	if string(d.Payload) != string(blocks[idx]) {
		t.Fatalf("singleton payload mismatch")
	}
	if err := d.Validate(uint32(k)); err != nil {
		t.Fatal(err)
	}
}

func TestGenerateDegree2Xor(t *testing.T) {
	k := 50
	blocks := testBlocks(k)
	params := NewEpochParams(0, uint32(k), [32]byte{42})
	encoder := NewEncoder(&params, fixedDegree(2), blocks)
	d := encoder.Generate(0)
	if len(d.Indices) != 2 {
		t.Fatalf("degree = %d, want 2", len(d.Indices))
	}
	want := xor.XorBlocks([][]byte{blocks[d.Indices[0]], blocks[d.Indices[1]]})
	if string(d.Payload) != string(want) {
		t.Fatalf("payload mismatch")
	}
}

func TestDeterministicGeneration(t *testing.T) {
	k := 100
	blocks := testBlocks(k)
	params := NewEpochParams(0, uint32(k), [32]byte{7})
	dist := distribution.NewRobustSoliton(k, 0.1, 0.05)
	encoder := NewEncoder(&params, dist, blocks)
	d1 := encoder.Generate(42)
	d2 := encoder.Generate(42)
	if string(d1.Payload) != string(d2.Payload) || len(d1.Indices) != len(d2.Indices) {
		t.Fatalf("droplets differ")
	}
	for i := range d1.Indices {
		if d1.Indices[i] != d2.Indices[i] {
			t.Fatalf("indices differ")
		}
	}
}

func TestDifferentDropletIDsDiffer(t *testing.T) {
	k := 100
	blocks := testBlocks(k)
	params := NewEpochParams(0, uint32(k), [32]byte{7})
	dist := distribution.NewRobustSoliton(k, 0.1, 0.05)
	encoder := NewEncoder(&params, dist, blocks)
	d1 := encoder.Generate(0)
	d2 := encoder.Generate(1)
	if len(d1.Indices) == len(d2.Indices) {
		same := true
		for i := range d1.Indices {
			if d1.Indices[i] != d2.Indices[i] {
				same = false
				break
			}
		}
		if same {
			t.Fatal("different droplet ids produced identical indices")
		}
	}
}

func TestGenerateN(t *testing.T) {
	k := 50
	blocks := testBlocks(k)
	params := NewEpochParams(0, uint32(k), [32]byte{})
	dist := distribution.NewRobustSoliton(k, 0.1, 0.5)
	encoder := NewEncoder(&params, dist, blocks)
	droplets := encoder.GenerateN(200)
	if len(droplets) != 200 {
		t.Fatalf("len = %d", len(droplets))
	}
	for id, d := range droplets {
		if d.DropletID != uint64(id) {
			t.Fatalf("droplet id = %d, want %d", d.DropletID, id)
		}
		if err := d.Validate(uint32(k)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestGenerateIntoMatchesGenerate(t *testing.T) {
	k := 20
	blocks := testBlocks(k)
	params := NewEpochParams(0, uint32(k), [32]byte{9})
	dist := distribution.NewRobustSoliton(k, 0.1, 0.5)
	encoder := NewEncoder(&params, dist, blocks)
	d := encoder.Generate(12)
	var indices []uint32
	payload := make([]byte, 1000)
	degree, paddedLen := encoder.GenerateInto(12, &indices, payload)
	if degree != len(d.Indices) || paddedLen != d.PaddedLen {
		t.Fatalf("metadata mismatch")
	}
	if string(payload[:paddedLen]) != string(d.Payload) {
		t.Fatalf("payload mismatch")
	}
}

func TestValidateCatchesBadDroplet(t *testing.T) {
	d := Droplet{Indices: []uint32{5, 3}, PaddedLen: 10, Payload: make([]byte, 10)}
	if d.Validate(100) == nil {
		t.Fatal("expected unsorted indices error")
	}
	d.Indices = []uint32{3, 105}
	if d.Validate(100) == nil {
		t.Fatal("expected out-of-range error")
	}
	d.Indices = []uint32{3, 50}
	if err := d.Validate(100); err != nil {
		t.Fatal(err)
	}
	d.PaddedLen = 20
	if d.Validate(100) == nil {
		t.Fatal("expected payload length error")
	}
}

func TestSingletonRecoversOriginalBlock(t *testing.T) {
	k := 30
	blocks := testBlocks(k)
	params := NewEpochParams(0, uint32(k), [32]byte{99})
	dist := distribution.NewRobustSoliton(k, 0.1, 0.5)
	encoder := NewEncoder(&params, dist, blocks)
	for _, d := range encoder.GenerateN(500) {
		if len(d.Indices) == 1 {
			idx := d.Indices[0]
			if string(d.Payload[:len(blocks[idx])]) != string(blocks[idx]) {
				t.Fatalf("singleton droplet %d doesn't match block %d", d.DropletID, idx)
			}
		}
	}
}
