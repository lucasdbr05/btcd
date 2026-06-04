package encode

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/btcsuite/btcd/fountain/droplet"
)

func sampleDroplet() droplet.Droplet {
	return droplet.Droplet{EpochID: 42, DropletID: 7, Indices: []uint32{3, 15, 99}, PaddedLen: 8, Payload: []byte{0xde, 0xad, 0xbe, 0xef, 0xca, 0xfe, 0xba, 0xbe}}
}

func TestRoundtrip(t *testing.T) {
	original := sampleDroplet()
	var buf bytes.Buffer
	if err := WriteDroplet(&buf, &original); err != nil {
		t.Fatal(err)
	}
	recovered, err := ReadDroplet(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.EpochID != original.EpochID || recovered.DropletID != original.DropletID || recovered.PaddedLen != original.PaddedLen || string(recovered.Payload) != string(original.Payload) {
		t.Fatalf("roundtrip mismatch")
	}
}

func TestSingletonRoundtrip(t *testing.T) {
	d := droplet.Droplet{Indices: []uint32{0}, PaddedLen: 4, Payload: []byte{1, 2, 3, 4}}
	var buf bytes.Buffer
	if err := WriteDroplet(&buf, &d); err != nil {
		t.Fatal(err)
	}
	recovered, err := ReadDroplet(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if len(recovered.Indices) != 1 || recovered.Indices[0] != 0 || string(recovered.Payload) != string([]byte{1, 2, 3, 4}) {
		t.Fatalf("roundtrip mismatch")
	}
}

func TestTruncatedPayload(t *testing.T) {
	d := sampleDroplet()
	var buf bytes.Buffer
	if err := WriteDroplet(&buf, &d); err != nil {
		t.Fatal(err)
	}
	data := buf.Bytes()
	_, err := ReadDroplet(bytes.NewReader(data[:len(data)-2]))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestLargeDropletRoundtrip(t *testing.T) {
	payloadSize := 1_000_000
	d := droplet.Droplet{EpochID: 100, DropletID: 999, Indices: []uint32{0, 50, 100, 200, 499}, PaddedLen: uint32(payloadSize), Payload: bytes.Repeat([]byte{0xab}, payloadSize)}
	var buf bytes.Buffer
	if err := WriteDroplet(&buf, &d); err != nil {
		t.Fatal(err)
	}
	recovered, err := ReadDroplet(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if len(recovered.Payload) != payloadSize || len(recovered.Indices) != 5 {
		t.Fatalf("roundtrip mismatch")
	}
}

func TestEmptyPayloadRoundtrip(t *testing.T) {
	d := droplet.Droplet{EpochID: 1, DropletID: 2, Indices: []uint32{0}, PaddedLen: 0, Payload: nil}
	var buf bytes.Buffer
	if err := WriteDroplet(&buf, &d); err != nil {
		t.Fatal(err)
	}
	recovered, err := ReadDroplet(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if len(recovered.Payload) != 0 || recovered.PaddedLen != 0 {
		t.Fatalf("roundtrip mismatch")
	}
}

func TestCorruptedData(t *testing.T) {
	if _, err := ReadDroplet(bytes.NewReader([]byte{0xff, 0xff})); err == nil {
		t.Fatal("expected error")
	}
}

func TestCompactSizeVarIntCompatibility(t *testing.T) {
	d := droplet.Droplet{Indices: make([]uint32, 253), PaddedLen: 253, Payload: make([]byte, 253)}
	var buf bytes.Buffer
	if err := WriteDroplet(&buf, &d); err != nil {
		t.Fatal(err)
	}
	data := buf.Bytes()
	if data[16] != 0xfd || data[17] != 0xfd || data[18] != 0x00 {
		t.Fatalf("indices varint is not Bitcoin CompactSize: %x", data[16:19])
	}
}

func TestWriteDropletFromParts(t *testing.T) {
	d := sampleDroplet()
	var fromStruct, fromParts bytes.Buffer
	if err := WriteDroplet(&fromStruct, &d); err != nil {
		t.Fatal(err)
	}
	if err := WriteDropletFromParts(&fromParts, d.EpochID, d.DropletID, d.Indices, d.PaddedLen, d.Payload); err != nil {
		t.Fatal(err)
	}
	if string(fromStruct.Bytes()) != string(fromParts.Bytes()) {
		t.Fatal("part encoding mismatch")
	}
}

func TestReadEpochDroplets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "epoch.dat")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	d1 := sampleDroplet()
	d2 := droplet.Droplet{Indices: []uint32{0}, PaddedLen: 1, Payload: []byte{1}}
	if err := WriteDroplet(f, &d1); err != nil {
		t.Fatal(err)
	}
	if err := WriteDroplet(f, &d2); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	got, err := ReadEpochDroplets(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d droplets", len(got))
	}
}
