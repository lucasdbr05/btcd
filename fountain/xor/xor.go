package xor

import "encoding/binary"

func XorBlocks(blocks [][]byte) []byte {
	maxLen := 0
	for _, b := range blocks {
		if len(b) > maxLen {
			maxLen = len(b)
		}
	}
	result := make([]byte, maxLen)
	for _, b := range blocks {
		xorPrefix(result, b)
	}
	return result
}

func XorIntoFixed(dst, src []byte) bool {
	if len(src) > len(dst) {
		return false
	}
	xorPrefix(dst, src)
	return true
}

func xorPrefix(dst, src []byte) {
	n := len(src)
	words := n / 8
	for i := range words {
		off := i * 8
		d := binary.LittleEndian.Uint64(dst[off:])
		s := binary.LittleEndian.Uint64(src[off:])
		binary.LittleEndian.PutUint64(dst[off:], d^s)
	}
	for i := words * 8; i < n; i++ {
		dst[i] ^= src[i]
	}
}
