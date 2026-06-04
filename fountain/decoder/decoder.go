package decoder

import (
	"container/list"
	"fmt"

	"github.com/btcsuite/btcd/fountain/droplet"
	"github.com/btcsuite/btcd/fountain/xor"
)

type PeelResult struct {
	Decoded    int
	Total      int
	Iterations int
	Success    bool
}

type DecodeStopReason int

const (
	StopCompleted DecodeStopReason = iota
	StopStalled
)

func (r DecodeStopReason) String() string {
	if r == StopCompleted {
		return "Completed"
	}
	return "Stalled"
}

type BlockFailure struct {
	Index        uint32
	ReferencedBy int
}

type DecodeResult struct {
	Blocks         []*[]byte
	K              int
	DecodedCount   int
	Iterations     int
	VerifyFailures int
	StopReason     DecodeStopReason
	Failures       []BlockFailure
}

func (r *DecodeResult) IsSuccess() bool {
	return r.DecodedCount == r.K && r.StopReason == StopCompleted
}

type VerifyError struct {
	Kind     string
	BlockIdx uint32
	Expected string
	Got      string
}

func (e *VerifyError) Error() string {
	switch e.Kind {
	case "hash_mismatch":
		return fmt.Sprintf("hash mismatch for block %d: expected %s, got %s", e.BlockIdx, e.Expected, e.Got)
	case "merkle_mismatch":
		return fmt.Sprintf("merkle mismatch for block %d: expected %s, got %s", e.BlockIdx, e.Expected, e.Got)
	case "deserialize":
		return fmt.Sprintf("deserialize error: %s", e.Expected)
	default:
		return e.Kind
	}
}

func NewVerifyError(kind string) *VerifyError { return &VerifyError{Kind: kind} }

type BlockVerifier interface {
	VerifyAndLen(blockIdx uint32, candidate []byte) (int, error)
}

type AcceptAllVerifier struct{}

func (AcceptAllVerifier) VerifyAndLen(_ uint32, candidate []byte) (int, error) {
	return len(candidate), nil
}

func PeelingCheck(k int, droplets [][]uint32) PeelResult {
	degree := make([]uint32, len(droplets))
	lastBlock := make([]uint32, len(droplets))
	for i, d := range droplets {
		degree[i] = uint32(len(d))
		if len(d) > 0 {
			lastBlock[i] = d[len(d)-1]
		}
	}

	blockToDroplets := make([][]int, k)
	for di, indices := range droplets {
		for _, idx := range indices {
			blockToDroplets[idx] = append(blockToDroplets[idx], di)
		}
	}

	queue := list.New()
	for di, deg := range degree {
		if deg == 1 {
			queue.PushBack(di)
		}
	}

	decoded := make([]bool, k)
	decodedCount := 0
	iterations := 0

	for queue.Len() > 0 {
		front := queue.Front()
		queue.Remove(front)
		di := front.Value.(int)

		if degree[di] != 1 {
			continue
		}
		blockIdx := lastBlock[di]
		if decoded[blockIdx] {
			continue
		}

		decoded[blockIdx] = true
		decodedCount++
		iterations++

		if decodedCount == k {
			break
		}

		refs := blockToDroplets[blockIdx]
		blockToDroplets[blockIdx] = nil
		for _, refDI := range refs {
			if refDI == di {
				continue
			}
			degree[refDI]--
			if degree[refDI] == 1 {
				for _, x := range droplets[refDI] {
					if !decoded[x] {
						lastBlock[refDI] = x
						break
					}
				}
				queue.PushBack(refDI)
			}
		}
	}

	return PeelResult{
		Decoded:    decodedCount,
		Total:      k,
		Iterations: iterations,
		Success:    decodedCount == k,
	}
}

func PeelingDecode(k int, droplets []droplet.Droplet, verifier BlockVerifier) DecodeResult {
	n := len(droplets)

	remainingDegree := make([]uint32, n)
	xorIndex := make([]uint32, n)
	payloads := make([][]byte, n)
	disabled := make([]bool, n)
	blockToDroplets := make([][]int, k)

	for i, d := range droplets {
		if err := d.Validate(uint32(k)); err != nil {
			disabled[i] = true
			payloads[i] = d.Payload
			continue
		}
		di := i
		deg := uint32(len(d.Indices))
		var xIdx uint32
		for _, idx := range d.Indices {
			xIdx ^= idx
			blockToDroplets[idx] = append(blockToDroplets[idx], di)
		}
		remainingDegree[i] = deg
		xorIndex[i] = xIdx
		payloads[i] = d.Payload
	}

	queue := list.New()
	for di, deg := range remainingDegree {
		if deg == 1 && !disabled[di] {
			queue.PushBack(di)
		}
	}

	blocks := make([]*[]byte, k)
	decodedCount := 0
	iterations := 0
	verifyFailures := 0

	for queue.Len() > 0 {
		front := queue.Front()
		queue.Remove(front)
		di := front.Value.(int)

		if disabled[di] || remainingDegree[di] != 1 {
			continue
		}

		blockIdx := xorIndex[di]
		if blocks[blockIdx] != nil {
			continue
		}

		candidate := payloads[di]
		payloads[di] = nil

		trueLen, err := verifier.VerifyAndLen(blockIdx, candidate)
		if err != nil {
			disabled[di] = true
			verifyFailures++
			payloads[di] = candidate
			continue
		}

		blockBytes := candidate[:trueLen]
		decodedCount++
		iterations++

		if decodedCount == k {
			b := append([]byte{}, blockBytes...)
			blocks[blockIdx] = &b
			break
		}

		refs := blockToDroplets[blockIdx]
		blockToDroplets[blockIdx] = nil
		for _, refDI := range refs {
			if refDI == di || disabled[refDI] {
				continue
			}
			if !xor.XorIntoFixed(payloads[refDI], blockBytes) {
				disabled[refDI] = true
				continue
			}
			remainingDegree[refDI]--
			xorIndex[refDI] ^= blockIdx
			if remainingDegree[refDI] == 1 {
				queue.PushBack(refDI)
			}
		}

		b := append([]byte{}, blockBytes...)
		blocks[blockIdx] = &b
	}

	var failures []BlockFailure
	for i := range k {
		if blocks[i] == nil {
			count := 0
			for _, di := range blockToDroplets[i] {
				if !disabled[di] {
					count++
				}
			}
			failures = append(failures, BlockFailure{Index: uint32(i), ReferencedBy: count})
		}
	}

	stopReason := StopStalled
	if decodedCount == k {
		stopReason = StopCompleted
	}

	return DecodeResult{
		Blocks:         blocks,
		K:              k,
		DecodedCount:   decodedCount,
		Iterations:     iterations,
		VerifyFailures: verifyFailures,
		StopReason:     stopReason,
		Failures:       failures,
	}
}
