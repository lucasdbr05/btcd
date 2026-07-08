// Copyright (c) 2026 The btcsuite developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wire

import "io"

// MsgSendFountain implements the Message interface and represents a bitcoin
// sendftn message. It is sent during the version-verack handshake to signal
// support for the Fountain Code sub-protocol (NODE_FOUNTAIN) and to negotiate
// the sub-protocol version.
type MsgSendFountain struct {
	// Version is the Fountain Code sub-protocol version supported by the
	// sender. Currently the only defined version is 1.
	Version uint8
}

// BtcDecode decodes r using the bitcoin protocol encoding into the receiver.
// This is part of the Message interface implementation.
func (msg *MsgSendFountain) BtcDecode(r io.Reader, pver uint32, enc MessageEncoding) error {
	return readElement(r, &msg.Version)
}

// BtcEncode encodes the receiver to w using the bitcoin protocol encoding.
// This is part of the Message interface implementation.
func (msg *MsgSendFountain) BtcEncode(w io.Writer, pver uint32, enc MessageEncoding) error {
	return writeElement(w, msg.Version)
}

// Command returns the protocol command string for the message. This is part
// of the Message interface implementation.
func (msg *MsgSendFountain) Command() string {
	return CmdSendFountain
}

// MaxPayloadLength returns the maximum length the payload can be for the
// receiver. This is part of the Message interface implementation.
func (msg *MsgSendFountain) MaxPayloadLength(pver uint32) uint32 {
	// version: uint8 (1 byte)
	return 1
}

// NewMsgSendFountain returns a new sendftn message that conforms to the
// Message interface. See MsgSendFountain for details.
func NewMsgSendFountain(version uint8) *MsgSendFountain {
	return &MsgSendFountain{Version: version}
}
