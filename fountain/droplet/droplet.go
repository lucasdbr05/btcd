package droplet

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math/rand/v2"
	"sort"

	"github.com/btcsuite/btcd/fountain/distribution"
	"github.com/btcsuite/btcd/fountain/xor"
)

type Droplet struct {
	EpochID   uint64
	DropletID uint64
	Indices   []uint32
	PaddedLen uint32
	Payload   []byte
}

type Encoder struct {
	params *EpochParams
	dist   distribution.DegreeDistribution
	blocks [][]byte
}

type EpochParams struct {
	EpochID   uint64
	K         uint32
	EpochSeed [32]byte
}

func (d *Droplet) Validate(k uint32) error {
	if len(d.Indices) == 0 {
		return fmt.Errorf("droplet has no indices")
	}
	if uint32(len(d.Payload)) != d.PaddedLen {
		return fmt.Errorf("payload length %d != padded_len %d", len(d.Payload), d.PaddedLen)
	}
	for i, idx := range d.Indices {
		if idx >= k {
			return fmt.Errorf("index %d >= k=%d", idx, k)
		}
		if i > 0 && idx <= d.Indices[i-1] {
			return fmt.Errorf("indices not strictly sorted at position %d", i)
		}
	}
	return nil
}

func NewEpochParams(epochID uint64, k uint32, seed [32]byte) EpochParams {
	return EpochParams{
		EpochID:   epochID,
		K:         k,
		EpochSeed: seed,
	}
}

func (p *EpochParams) DeriveIndices(dist distribution.DegreeDistribution,
	dropletID uint64) []uint32 {

	k := int(p.K)
	rng := p.dropletRNG(dropletID)
	degree := dist.SampleDegree(rng)
	return sampleIndices(rng, k, degree)
}

func (p *EpochParams) dropletRNG(dropletID uint64) *rand.Rand {
	h := sha256.New()
	h.Write(p.EpochSeed[:])
	h.Write([]byte("droplet"))
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], p.EpochID)
	h.Write(buf[:])
	binary.LittleEndian.PutUint64(buf[:], dropletID)
	h.Write(buf[:])
	seed := h.Sum(nil)

	s1 := binary.LittleEndian.Uint64(seed[:8])
	s2 := binary.LittleEndian.Uint64(seed[8:16])
	return rand.New(rand.NewPCG(s1, s2))
}

func NewEncoder(params *EpochParams, dist distribution.DegreeDistribution, blocks [][]byte) *Encoder {
	if len(blocks) != int(params.K) {
		panic(fmt.Sprintf("blocks len %d != k %d", len(blocks), params.K))
	}
	return &Encoder{params: params, dist: dist, blocks: blocks}
}

func (e *Encoder) Generate(dropletID uint64) Droplet {
	indices := e.params.DeriveIndices(e.dist, dropletID)

	selected := make([][]byte, len(indices))
	var paddedLen int
	for i, idx := range indices {
		b := e.blocks[idx]
		selected[i] = b
		if len(b) > paddedLen {
			paddedLen = len(b)
		}
	}

	payload := xor.XorBlocks(selected)

	return Droplet{
		EpochID:   e.params.EpochID,
		DropletID: dropletID,
		Indices:   indices,
		PaddedLen: uint32(paddedLen),
		Payload:   payload,
	}
}

func (e *Encoder) GenerateInto(dropletID uint64, indicesBuf *[]uint32, payloadBuf []byte) (int, uint32) {
	indices := e.params.DeriveIndices(e.dist, dropletID)
	*indicesBuf = append((*indicesBuf)[:0], indices...)

	paddedLen := 0
	for _, idx := range *indicesBuf {
		if l := len(e.blocks[idx]); l > paddedLen {
			paddedLen = l
		}
	}
	if len(payloadBuf) < paddedLen {
		panic(fmt.Sprintf("payload buffer len %d < padded len %d", len(payloadBuf), paddedLen))
	}

	dst := payloadBuf[:paddedLen]
	for i := range dst {
		dst[i] = 0
	}
	for _, idx := range *indicesBuf {
		if !xor.XorIntoFixed(dst, e.blocks[idx]) {
			panic("source block longer than padded payload")
		}
	}

	return len(indices), uint32(paddedLen)
}

func (e *Encoder) GenerateN(n uint64) []Droplet {
	out := make([]Droplet, n)
	for i := range n {
		out[i] = e.Generate(i)
	}
	return out
}

// sampleIndices implements Fisher-Yates partial shuffle to select `count`
// unique indices from [0, k) in sorted order.
func sampleIndices(rng *rand.Rand, k, count int) []uint32 {
	if count > k {
		count = k
	}
	perm := make([]int, k)
	for i := range perm {
		perm[i] = i
	}
	for i := range count {
		j := i + int(rng.Uint64()%uint64(k-i))
		perm[i], perm[j] = perm[j], perm[i]
	}
	result := make([]uint32, count)
	for i := range count {
		result[i] = uint32(perm[i])
	}
	sortUint32(result)
	return result
}

func sortUint32(s []uint32) {
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
}
