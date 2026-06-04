package xor

import "testing"

func TestXorBlocksMultiple(t *testing.T) {
	a := []byte{0xff, 0x00, 0xaa}
	b := []byte{0x0f, 0xf0}
	c := []byte{0x01, 0x02, 0x03, 0x04}
	got := XorBlocks([][]byte{a, b, c})
	want := []byte{0xf1, 0xf2, 0xa9, 0x04}
	if string(got) != string(want) {
		t.Fatalf("got %x want %x", got, want)
	}
}

func TestBlocksSingle(t *testing.T) {
	a := []byte{0xde, 0xad}
	got := XorBlocks([][]byte{a})
	if string(got) != string(a) {
		t.Fatalf("got %x want %x", got, a)
	}
}

func TestXorBlocksEmpty(t *testing.T) {
	if got := XorBlocks(nil); len(got) != 0 {
		t.Fatalf("got len %d want 0", len(got))
	}
}

func TestXorBlocksUnalignedInputs(t *testing.T) {
	aBacking := append([]byte{0xaa}, []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}...)
	bBacking := append([]byte{0xbb}, []byte{17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33}...)
	a := aBacking[1:]
	b := bBacking[1:]

	gotBlocks := XorBlocks([][]byte{a, b})
	gotFixed := append([]byte{}, a...)
	if !XorIntoFixed(gotFixed, b) {
		t.Fatal("XorIntoFixed returned false")
	}
	if string(gotBlocks) != string(gotFixed) {
		t.Fatalf("got %x want %x", gotBlocks, gotFixed)
	}
}

func TestXorIntoFixedRejectsOversizedSrcWithoutMutating(t *testing.T) {
	dst := []byte{0x12, 0x34}
	if XorIntoFixed(dst, []byte{0xaa, 0xbb, 0xcc}) {
		t.Fatal("expected oversized src rejection")
	}
	if string(dst) != string([]byte{0x12, 0x34}) {
		t.Fatalf("dst mutated: %x", dst)
	}
}
