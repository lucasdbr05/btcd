// Copyright (c) 2026 The btcsuite developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wire

import "io"

// MsgGetFtnEpochs implements the Message interface and represents a bitcoin
// getftnepochs message. It is used to request the full list of Fountain Code
// epochs a peer has available for relay.

type MsgGetFtnEpochs struct{}

// BtcDecode decodes r using the bitcoin protocol encoding into the receiver.
// This is part of the Message interface implementation.
func (msg *MsgGetFtnEpochs) BtcDecode(r io.Reader, pver uint32, enc MessageEncoding) error {
	return nil
}

// BtcEncode encodes the receiver to w using the bitcoin protocol encoding.
// This is part of the Message interface implementation.
func (msg *MsgGetFtnEpochs) BtcEncode(w io.Writer, pver uint32, enc MessageEncoding) error {
	return nil
}

// Command returns the protocol command string for the message. This is part
// of the Message interface implementation.
func (msg *MsgGetFtnEpochs) Command() string {
	return CmdGetFtnEpochs
}

// MaxPayloadLength returns the maximum length the payload can be for the
// receiver. This is part of the Message interface implementation.
func (msg *MsgGetFtnEpochs) MaxPayloadLength(pver uint32) uint32 {
	return 0
}

// NewMsgGetFtnEpochs returns a new getftnepochs message that conforms to the
// Message interface. See MsgGetFtnEpochs for details.
func NewMsgGetFtnEpochs() *MsgGetFtnEpochs {
	return &MsgGetFtnEpochs{}
}
