// Copyright (c) 2026 The btcsuite developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wire

import (
	"fmt"
	"io"
)

func readDifferentialIndex(r io.Reader, pver uint32, buf []byte,
	lastIndex uint64) (uint64, error) {

	differential, err := ReadVarIntBuf(r, pver, buf)
	if err != nil {
		return 0, err
	}

	if lastIndex == uint64(^uint32(0)) {
		lastIndex = 0
	} else {
		lastIndex++
	}

	index := lastIndex + differential
	if index > uint64(^uint32(0)) {
		str := fmt.Sprintf("differential index overflows uint32 "+
			"[index %v]", index)
		return 0, messageError("readDifferentialIndex", str)
	}

	return index, nil
}

func writeDifferentialIndex(w io.Writer, pver uint32, buf []byte, index,
	lastIndex uint64) error {

	var differential uint64
	if lastIndex == uint64(^uint32(0)) {
		differential = index
	} else {
		if index <= lastIndex {
			str := fmt.Sprintf("indexes must be strictly increasing "+
				"[index %v, last %v]", index, lastIndex)
			return messageError("writeDifferentialIndex", str)
		}
		differential = index - lastIndex - 1
	}

	return WriteVarIntBuf(w, pver, differential, buf)
}
