package encode

import (
	"encoding/binary"
	"io"
	"os"

	"github.com/btcsuite/btcd/fountain/droplet"
)

func WriteDroplet(w io.Writer, d *droplet.Droplet) error {
	if err := writeU64(w, d.EpochID); err != nil {
		return err
	}
	if err := writeU64(w, d.DropletID); err != nil {
		return err
	}
	if err := writeVarInt(w, uint64(len(d.Indices))); err != nil {
		return err
	}
	for _, idx := range d.Indices {
		if err := writeU32(w, idx); err != nil {
			return err
		}
	}
	if err := writeU32(w, d.PaddedLen); err != nil {
		return err
	}
	if err := writeVarInt(w, uint64(len(d.Payload))); err != nil {
		return err
	}
	_, err := w.Write(d.Payload)
	return err
}

func WriteDropletFromParts(w io.Writer, epochID, dropletID uint64, indices []uint32, paddedLen uint32, payload []byte) error {
	if err := writeU64(w, epochID); err != nil {
		return err
	}
	if err := writeU64(w, dropletID); err != nil {
		return err
	}
	if err := writeVarInt(w, uint64(len(indices))); err != nil {
		return err
	}
	for _, idx := range indices {
		if err := writeU32(w, idx); err != nil {
			return err
		}
	}
	if err := writeU32(w, paddedLen); err != nil {
		return err
	}
	if err := writeVarInt(w, uint64(len(payload))); err != nil {
		return err
	}
	_, err := w.Write(payload)
	return err
}

func ReadDroplet(r io.Reader) (droplet.Droplet, error) {
	epochID, err := readU64(r)
	if err != nil {
		return droplet.Droplet{}, err
	}
	dropletID, err := readU64(r)
	if err != nil {
		return droplet.Droplet{}, err
	}
	numIndices, err := readVarInt(r)
	if err != nil {
		return droplet.Droplet{}, err
	}
	indices := make([]uint32, numIndices)
	for i := range numIndices {
		idx, err := readU32(r)
		if err != nil {
			return droplet.Droplet{}, err
		}
		indices[i] = idx
	}
	paddedLen, err := readU32(r)
	if err != nil {
		return droplet.Droplet{}, err
	}
	payloadLen, err := readVarInt(r)
	if err != nil {
		return droplet.Droplet{}, err
	}
	payload := make([]byte, payloadLen)
	if _, err := io.ReadFull(r, payload); err != nil {
		return droplet.Droplet{}, err
	}
	return droplet.Droplet{
		EpochID:   epochID,
		DropletID: dropletID,
		Indices:   indices,
		PaddedLen: paddedLen,
		Payload:   payload,
	}, nil
}

func ReadEpochDroplets(path string) ([]droplet.Droplet, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []droplet.Droplet
	for {
		d, err := ReadDroplet(f)
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			break
		}
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

func writeU64(w io.Writer, v uint64) error {
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], v)
	_, err := w.Write(buf[:])
	return err
}

func writeU32(w io.Writer, v uint32) error {
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], v)
	_, err := w.Write(buf[:])
	return err
}

func writeVarInt(w io.Writer, v uint64) error {
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
	_, err := w.Write(buf[:n])
	return err
}

func readU64(r io.Reader) (uint64, error) {
	var buf [8]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint64(buf[:]), nil
}

func readU32(r io.Reader) (uint32, error) {
	var buf [4]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(buf[:]), nil
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
