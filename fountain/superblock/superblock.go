package superblock

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/btcsuite/btcd/wire"
)

type Manifest struct {
	TotalBlocks int
	TotalSupers int
	BlockCounts []int
}

type EpochManifest struct {
	EpochID  uint64
	Manifest Manifest
}

func BlocksToSuperblocks(blocks [][]byte, targetBytes int) ([][]byte, []int) {
	if targetBytes <= 0 {
		panic("superblock: targetBytes must be > 0")
	}
	if len(blocks) == 0 {
		return nil, nil
	}

	var supers [][]byte
	var counts []int

	cur := make([]byte, 0, targetBytes)
	curCount := 0

	for _, block := range blocks {
		if len(cur) > 0 && len(cur)+len(block) > targetBytes {
			cp := make([]byte, len(cur))
			copy(cp, cur)
			supers = append(supers, cp)
			counts = append(counts, curCount)
			cur = cur[:0]
			curCount = 0
		}
		cur = append(cur, block...)
		curCount++
	}

	if len(cur) > 0 {
		cp := make([]byte, len(cur))
		copy(cp, cur)
		supers = append(supers, cp)
		counts = append(counts, curCount)
	}

	return supers, counts
}

// SuperblocksToBlocks reassembles individual blocks from decoded superblocks.
//
// For each superblock that is non-nil, uses wire.MsgBlock.Deserialize to
// sequentially parse the expected number of blocks (from blockCounts).
// Returns nil for blocks whose superblock was not recovered.
func SuperblocksToBlocks(superblocks []*[]byte, blockCounts []int, totalBlocks int) []*[]byte {
	result := make([]*[]byte, totalBlocks)

	blockOffset := 0
	for si, sb := range superblocks {
		count := blockCounts[si]
		if sb == nil {
			blockOffset += count
			continue
		}

		data := *sb
		offset := 0
		for i := 0; i < count; i++ {
			if offset >= len(data) {
				break
			}
			r := bytes.NewReader(data[offset:])
			var msg wire.MsgBlock
			if err := msg.Deserialize(r); err != nil {
				break
			}
			consumed := len(data[offset:]) - r.Len()
			cp := make([]byte, consumed)
			copy(cp, data[offset:offset+consumed])
			result[blockOffset+i] = &cp
			offset += consumed
		}
		blockOffset += count
	}

	return result
}

func SerializeManifests(epochs []EpochManifest) []byte {
	var buf bytes.Buffer
	writeVarInt(&buf, uint64(len(epochs)))
	for _, e := range epochs {
		writeU64(&buf, e.EpochID)
		writeVarInt(&buf, uint64(e.Manifest.TotalBlocks))
		writeVarInt(&buf, uint64(e.Manifest.TotalSupers))
		for _, c := range e.Manifest.BlockCounts {
			writeVarInt(&buf, uint64(c))
		}
	}
	return buf.Bytes()
}

func DeserializeManifests(data []byte) ([]EpochManifest, error) {
	r := bytes.NewReader(data)
	numEpochs, err := readVarInt(r)
	if err != nil {
		return nil, fmt.Errorf("reading num_epochs: %w", err)
	}

	epochs := make([]EpochManifest, numEpochs)
	for i := uint64(0); i < numEpochs; i++ {
		epochID, err := readU64(r)
		if err != nil {
			return nil, fmt.Errorf("epoch %d: reading epoch_id: %w", i, err)
		}
		totalBlocks, err := readVarInt(r)
		if err != nil {
			return nil, fmt.Errorf("epoch %d: reading total_blocks: %w", i, err)
		}
		totalSupers, err := readVarInt(r)
		if err != nil {
			return nil, fmt.Errorf("epoch %d: reading total_supers: %w", i, err)
		}
		blockCounts := make([]int, totalSupers)
		for j := uint64(0); j < totalSupers; j++ {
			c, err := readVarInt(r)
			if err != nil {
				return nil, fmt.Errorf("epoch %d super %d: reading block_count: %w", i, j, err)
			}
			blockCounts[j] = int(c)
		}
		epochs[i] = EpochManifest{
			EpochID: epochID,
			Manifest: Manifest{
				TotalBlocks: int(totalBlocks),
				TotalSupers: int(totalSupers),
				BlockCounts: blockCounts,
			},
		}
	}
	return epochs, nil
}

func writeU64(w io.Writer, v uint64) {
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], v)
	_, _ = w.Write(buf[:])
}

func readU64(r io.Reader) (uint64, error) {
	var buf [8]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint64(buf[:]), nil
}

func writeVarInt(w io.Writer, v uint64) {
	var buf [9]byte
	var n int
	switch {
	case v < 0xfd:
		buf[0] = byte(v)
		n = 1
	case v <= 0xffff:
		buf[0] = 0xfd
		binary.LittleEndian.PutUint16(buf[1:], uint16(v))
		n = 3
	case v <= 0xffffffff:
		buf[0] = 0xfe
		binary.LittleEndian.PutUint32(buf[1:], uint32(v))
		n = 5
	default:
		buf[0] = 0xff
		binary.LittleEndian.PutUint64(buf[1:], v)
		n = 9
	}
	_, _ = w.Write(buf[:n])
}

func readVarInt(r io.Reader) (uint64, error) {
	var prefix [1]byte
	if _, err := io.ReadFull(r, prefix[:]); err != nil {
		return 0, err
	}
	switch prefix[0] {
	case 0xfd:
		var buf [2]byte
		if _, err := io.ReadFull(r, buf[:]); err != nil {
			return 0, err
		}
		return uint64(binary.LittleEndian.Uint16(buf[:])), nil
	case 0xfe:
		var buf [4]byte
		if _, err := io.ReadFull(r, buf[:]); err != nil {
			return 0, err
		}
		return uint64(binary.LittleEndian.Uint32(buf[:])), nil
	case 0xff:
		var buf [8]byte
		if _, err := io.ReadFull(r, buf[:]); err != nil {
			return 0, err
		}
		return binary.LittleEndian.Uint64(buf[:]), nil
	default:
		return uint64(prefix[0]), nil
	}
}
