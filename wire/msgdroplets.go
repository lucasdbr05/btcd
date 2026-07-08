// Copyright (c) 2026 The btcsuite developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wire

import (
	"fmt"
	"io"
)

const (
	// droplet_id(8) + padded_len(4) + payload_len varint(9) +
	// max payload (4 MB cap shared with MaxMessagePayload).
	maxDropletPayload = 8 + 4 + MaxVarIntPayload + MaxMessagePayload
)

// DropletRecord is a single encoded Fountain Code droplet as transmitted on
// the wire.
type DropletRecord struct {
	// DropletID is the sequential identifier of this droplet within the
	// epoch. Together with the epoch seed it uniquely determines the
	// neighbour indices.
	DropletID uint64

	// PaddedLen is the byte length to which each source symbol was padded
	// before XOR-combining. Required by the decoder to strip padding.
	PaddedLen uint32

	// Payload of this droplet.
	Payload []byte
}

// MsgDroplets implements the Message interface and represents a bitcoin
// droplets message. It carries a batch of Fountain Code droplets for a single
// epoch, sent in response to getdroplets or pushed proactively.
type MsgDroplets struct {
	// EpochID identifies the epoch all droplets in this message belong to.
	EpochID uint64

	// Droplets is the list of encoded droplet records.
	Droplets []*DropletRecord
}

// AddDroplet appends a droplet to the message, returning an error if the
// maximum number of droplets per message would be exceeded.
func (msg *MsgDroplets) AddDroplet(d *DropletRecord) error {
	if len(msg.Droplets)+1 > MaxDropletsPerRequest {
		str := fmt.Sprintf("too many droplets in message [max %v]",
			MaxDropletsPerRequest)
		return messageError("MsgDroplets.AddDroplet", str)
	}
	msg.Droplets = append(msg.Droplets, d)
	return nil
}

// BtcDecode decodes r using the bitcoin protocol encoding into the receiver.
// This is part of the Message interface implementation.
func (msg *MsgDroplets) BtcDecode(r io.Reader, pver uint32, enc MessageEncoding) error {
	buf := binarySerializer.Borrow()
	defer binarySerializer.Return(buf)

	if err := readElement(r, &msg.EpochID); err != nil {
		return err
	}

	count, err := ReadVarIntBuf(r, pver, buf)
	if err != nil {
		return err
	}
	if count > MaxDropletsPerRequest {
		str := fmt.Sprintf("too many droplets in message [%v, max %v]",
			count, MaxDropletsPerRequest)
		return messageError("MsgDroplets.BtcDecode", str)
	}

	droplets := make([]DropletRecord, count)
	msg.Droplets = make([]*DropletRecord, 0, count)
	for i := uint64(0); i < count; i++ {
		d := &droplets[i]
		if err := readElements(r, &d.DropletID, &d.PaddedLen); err != nil {
			return err
		}
		payloadLen, err := ReadVarIntBuf(r, pver, buf)
		if err != nil {
			return err
		}
		if payloadLen > MaxMessagePayload {
			str := fmt.Sprintf("droplet payload too large [%v, max %v]",
				payloadLen, MaxMessagePayload)
			return messageError("MsgDroplets.BtcDecode", str)
		}
		d.Payload = make([]byte, payloadLen)
		if _, err := io.ReadFull(r, d.Payload); err != nil {
			return err
		}
		msg.Droplets = append(msg.Droplets, d)
	}
	return nil
}

// BtcEncode encodes the receiver to w using the bitcoin protocol encoding.
// This is part of the Message interface implementation.
func (msg *MsgDroplets) BtcEncode(w io.Writer, pver uint32, enc MessageEncoding) error {
	count := len(msg.Droplets)
	if count > MaxDropletsPerRequest {
		str := fmt.Sprintf("too many droplets in message [%v, max %v]",
			count, MaxDropletsPerRequest)
		return messageError("MsgDroplets.BtcEncode", str)
	}

	buf := binarySerializer.Borrow()
	defer binarySerializer.Return(buf)

	if err := writeElement(w, msg.EpochID); err != nil {
		return err
	}
	if err := WriteVarIntBuf(w, pver, uint64(count), buf); err != nil {
		return err
	}
	for _, d := range msg.Droplets {
		if err := writeElements(w, d.DropletID, d.PaddedLen); err != nil {
			return err
		}
		if err := WriteVarIntBuf(w, pver, uint64(len(d.Payload)), buf); err != nil {
			return err
		}
		if _, err := w.Write(d.Payload); err != nil {
			return err
		}
	}
	return nil
}

// Command returns the protocol command string for the message. This is part
// of the Message interface implementation.
func (msg *MsgDroplets) Command() string {
	return CmdDroplets
}

// MaxPayloadLength returns the maximum length the payload can be for the
// receiver. This is part of the Message interface implementation.
func (msg *MsgDroplets) MaxPayloadLength(pver uint32) uint32 {
	// Droplet payloads are bounded by the global message size cap.
	return MaxMessagePayload
}

// NewMsgDroplets returns a new droplets message that conforms to the Message
// interface. See MsgDroplets for details.
func NewMsgDroplets(epochID uint64) *MsgDroplets {
	return &MsgDroplets{
		EpochID:  epochID,
		Droplets: make([]*DropletRecord, 0),
	}
}
