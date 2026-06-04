package epoch

import (
	"crypto/sha256"
	"encoding/binary"
	"math"
)

type EpochConfig struct {
	K              int
	N              uint64
	Buffer         int
	C              float64
	Delta          float64
	SymbolSize     int
	SuperblockSize int
}

func ComputeEpochSeed(epochIdx int, firstBlockHash string) [32]byte {
	h := sha256.New()
	h.Write([]byte("epoch_seed"))
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], uint64(epochIdx))
	h.Write(buf[:])
	h.Write([]byte(firstBlockHash))
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

func AutoScaleDroplets(unitK int, n uint64) uint64 {
	if n != 0 {
		return n
	}
	k := float64(unitK)
	delta := 0.05
	safety := 2.5
	overhead := safety * math.Sqrt(k) * math.Log(k/delta)
	result := uint64(math.Ceil(k + overhead))
	if result <= uint64(unitK) {
		result = uint64(unitK) + 1
	}
	return result
}
