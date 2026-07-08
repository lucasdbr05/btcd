// Copyright (c) 2026 The btcsuite developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wire

import (
	"fmt"
	"io"
)

const (
	// MaxDropletsPerRequest is the maximum number of droplets that can be
	// requested in a single getdroplets message.
	MaxDropletsPerRequest = 100
)

// MsgGetDroplets implements the Message interface and represents a bitcoin
// getdroplets message. It is used to request a contiguous range of Fountain
// Code droplets from a peer for a given epoch.
//
// The range is expressed as [StartID, StartID+Count). Because droplet
// neighbour indices are derived deterministically from EpochSeed and DropletID,
// the responder needs only this range to generate or look up the requested
// droplets — no explicit index vectors are exchanged.
type MsgGetDroplets struct {
	// EpochID identifies the epoch for which droplets are requested.
	EpochID uint64

	// StartID is the first droplet_id in the requested range.
	StartID uint64

	// Count is the number of consecutive droplets requested, starting at
	// StartID. Must be in [1, MaxDropletsPerRequest].
	Count uint64
}

// BtcDecode decodes r using the bitcoin protocol encoding into the receiver.
// This is part of the Message interface implementation.
func (msg *MsgGetDroplets) BtcDecode(r io.Reader, pver uint32, enc MessageEncoding) error {
	if err := readElements(r, &msg.EpochID, &msg.StartID, &msg.Count); err != nil {
		return err
	}
	if msg.Count == 0 || msg.Count > MaxDropletsPerRequest {
		str := fmt.Sprintf("droplet count out of range [%v, max %v]",
			msg.Count, MaxDropletsPerRequest)
		return messageError("MsgGetDroplets.BtcDecode", str)
	}
	return nil
}

// BtcEncode encodes the receiver to w using the bitcoin protocol encoding.
// This is part of the Message interface implementation.
func (msg *MsgGetDroplets) BtcEncode(w io.Writer, pver uint32, enc MessageEncoding) error {
	if msg.Count == 0 || msg.Count > MaxDropletsPerRequest {
		str := fmt.Sprintf("droplet count out of range [%v, max %v]",
			msg.Count, MaxDropletsPerRequest)
		return messageError("MsgGetDroplets.BtcEncode", str)
	}
	return writeElements(w, msg.EpochID, msg.StartID, msg.Count)
}

// Command returns the protocol command string for the message. This is part
// of the Message interface implementation.
func (msg *MsgGetDroplets) Command() string {
	return CmdGetDroplets
}

// MaxPayloadLength returns the maximum length the payload can be for the
// receiver. This is part of the Message interface implementation.
func (msg *MsgGetDroplets) MaxPayloadLength(pver uint32) uint32 {
	// epoch_id(8) + start_id(8) + count(8)
	return 8 + 8 + 8
}

// NewMsgGetDroplets returns a new getdroplets message that conforms to the
// Message interface. See MsgGetDroplets for details.
func NewMsgGetDroplets(epochID, startID uint64, count uint64) *MsgGetDroplets {
	return &MsgGetDroplets{
		EpochID: epochID,
		StartID: startID,
		Count:   count,
	}
}
