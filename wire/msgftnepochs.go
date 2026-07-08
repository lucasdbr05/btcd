// Copyright (c) 2026 The btcsuite developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wire

import (
	"fmt"
	"io"
)

const (
	// MaxFtnEpochsPerMsg is the maximum number of Fountain Code epochs
	// that can be announced in a single ftnepochs message.
	MaxFtnEpochsPerMsg = 2048

	// ftnEpochPayload is the fixed size of a single FtnEpochInfo record
	// epoch_id(8) + epoch_seed(32) + num_droplets(8)
	ftnEpochPayload = 8 + 32 + 8
)

// FtnEpochInfo describes a single Fountain Code epoch available for relay.
type FtnEpochInfo struct {
	// EpochID is the numeric identifier of the epoch.
	EpochID uint64

	// EpochSeed is the 32-byte deterministic seed used to derive droplet
	// neighbour indices via dropletRNG(EpochSeed, DropletID).
	EpochSeed [32]byte

	// NumDroplets is the total number of droplets the peer has generated
	// (and can serve) for this epoch.
	NumDroplets uint64
}

// MsgFtnEpochs implements the Message interface and represents a bitcoin
// ftnepochs message. It is used to announce the Fountain Code epochs a node
// has available for relay. It may be sent unsolicited or in response to a
// getftnepochs message.
type MsgFtnEpochs struct {
	Epochs []*FtnEpochInfo
}

// AddEpoch appends an epoch to the message, returning an error if the
// maximum number of epochs per message would be exceeded.
func (msg *MsgFtnEpochs) AddEpoch(e *FtnEpochInfo) error {
	if len(msg.Epochs)+1 > MaxFtnEpochsPerMsg {
		str := fmt.Sprintf("too many epochs in message [max %v]",
			MaxFtnEpochsPerMsg)
		return messageError("MsgFtnEpochs.AddEpoch", str)
	}
	msg.Epochs = append(msg.Epochs, e)
	return nil
}

// BtcDecode decodes r using the bitcoin protocol encoding into the receiver.
// This is part of the Message interface implementation.
func (msg *MsgFtnEpochs) BtcDecode(r io.Reader, pver uint32, enc MessageEncoding) error {
	buf := binarySerializer.Borrow()
	defer binarySerializer.Return(buf)

	count, err := ReadVarIntBuf(r, pver, buf)
	if err != nil {
		return err
	}
	if count > MaxFtnEpochsPerMsg {
		str := fmt.Sprintf("too many epochs in message [%v, max %v]",
			count, MaxFtnEpochsPerMsg)
		return messageError("MsgFtnEpochs.BtcDecode", str)
	}

	epochs := make([]FtnEpochInfo, count)
	msg.Epochs = make([]*FtnEpochInfo, 0, count)
	for i := uint64(0); i < count; i++ {
		e := &epochs[i]
		if err := readElements(r, &e.EpochID, &e.EpochSeed, &e.NumDroplets); err != nil {
			return err
		}
		msg.Epochs = append(msg.Epochs, e)
	}
	return nil
}

// BtcEncode encodes the receiver to w using the bitcoin protocol encoding.
// This is part of the Message interface implementation.
func (msg *MsgFtnEpochs) BtcEncode(w io.Writer, pver uint32, enc MessageEncoding) error {
	count := len(msg.Epochs)
	if count > MaxFtnEpochsPerMsg {
		str := fmt.Sprintf("too many epochs in message [%v, max %v]",
			count, MaxFtnEpochsPerMsg)
		return messageError("MsgFtnEpochs.BtcEncode", str)
	}

	buf := binarySerializer.Borrow()
	defer binarySerializer.Return(buf)

	if err := WriteVarIntBuf(w, pver, uint64(count), buf); err != nil {
		return err
	}
	for _, e := range msg.Epochs {
		if err := writeElements(w, e.EpochID, e.EpochSeed, e.NumDroplets); err != nil {
			return err
		}
	}
	return nil
}

// Command returns the protocol command string for the message. This is part
// of the Message interface implementation.
func (msg *MsgFtnEpochs) Command() string {
	return CmdFtnEpochs
}

// MaxPayloadLength returns the maximum length the payload can be for the
// receiver. This is part of the Message interface implementation.
func (msg *MsgFtnEpochs) MaxPayloadLength(pver uint32) uint32 {
	// varint count + MaxFtnEpochsPerMsg records
	return MaxVarIntPayload + (MaxFtnEpochsPerMsg * ftnEpochPayload)
}

// NewMsgFtnEpochs returns a new ftnepochs message that conforms to the
// Message interface. See MsgFtnEpochs for details.
func NewMsgFtnEpochs() *MsgFtnEpochs {
	return &MsgFtnEpochs{
		Epochs: make([]*FtnEpochInfo, 0),
	}
}
