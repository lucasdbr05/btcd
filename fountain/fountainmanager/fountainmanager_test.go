package manager

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/database"
	_ "github.com/btcsuite/btcd/database/ffldb"
	"github.com/btcsuite/btcd/wire"
)

func testBlock(height int32, nonce uint32) *btcutil.Block {
	msg := &wire.MsgBlock{
		Header: wire.BlockHeader{
			Nonce: nonce,
		},
	}
	block := btcutil.NewBlock(msg)
	block.SetHeight(height)
	return block
}

func TestProcessBlockCompletesEpoch(t *testing.T) {
	mgr, err := New(Config{
		K:       3,
		S:       2,
		C:       0.3,
		Delta:   0.05,
		Version: 1,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	for i := int32(0); i < 2; i++ {
		_, completed, err := mgr.ProcessBlock(testBlock(i, uint32(i)))
		if err != nil {
			t.Fatalf("ProcessBlock(%d): %v", i, err)
		}
		if completed {
			t.Fatalf("epoch completed before k blocks")
		}
	}

	info, completed, err := mgr.ProcessBlock(testBlock(2, 2))
	if err != nil {
		t.Fatalf("ProcessBlock(2): %v", err)
	}
	if !completed {
		t.Fatalf("epoch not completed after k blocks")
	}
	if info.EpochID != 0 {
		t.Fatalf("epoch id = %d, want 0", info.EpochID)
	}
	if info.NumDroplets != 2 {
		t.Fatalf("num droplets = %d, want 2", info.NumDroplets)
	}

	msg, err := mgr.DropletsMessage(0, 0, 2)
	if err != nil {
		t.Fatalf("DropletsMessage: %v", err)
	}
	if len(msg.Droplets) != 2 {
		t.Fatalf("droplets len = %d, want 2", len(msg.Droplets))
	}
	for i, rec := range msg.Droplets {
		if rec.DropletID != uint64(i) {
			t.Fatalf("droplet id = %d, want %d", rec.DropletID, i)
		}
		if len(rec.Payload) == 0 {
			t.Fatalf("droplet %d has empty payload", i)
		}
		if uint32(len(rec.Payload)) != rec.PaddedLen {
			t.Fatalf("droplet payload len = %d, padded len = %d",
				len(rec.Payload), rec.PaddedLen)
		}
	}
}

func TestProcessBlockWaitsForEpochBoundary(t *testing.T) {
	mgr, err := New(Config{
		K:       3,
		S:       1,
		C:       0.3,
		Delta:   0.05,
		Version: 1,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, completed, err := mgr.ProcessBlock(testBlock(1, 1))
	if err != nil {
		t.Fatalf("ProcessBlock(1): %v", err)
	}
	if completed {
		t.Fatalf("mid-epoch block unexpectedly completed epoch")
	}
	if epochs := mgr.AvailableEpochs(); len(epochs) != 0 {
		t.Fatalf("available epochs = %d, want 0", len(epochs))
	}

	for i := int32(3); i <= 5; i++ {
		_, _, err := mgr.ProcessBlock(testBlock(i, uint32(i)))
		if err != nil {
			t.Fatalf("ProcessBlock(%d): %v", i, err)
		}
	}
	info, ok := mgr.EpochInfo(1)
	if !ok {
		t.Fatalf("epoch 1 not found")
	}
	if info.NumDroplets != 1 {
		t.Fatalf("num droplets = %d, want 1", info.NumDroplets)
	}
}

func TestProcessBlockSeedsAreIndependentAcrossManagers(t *testing.T) {
	config := Config{
		K:       3,
		S:       2,
		C:       0.3,
		Delta:   0.05,
		Version: 1,
	}
	first, err := New(config)
	if err != nil {
		t.Fatalf("New first: %v", err)
	}
	second, err := New(config)
	if err != nil {
		t.Fatalf("New second: %v", err)
	}
	first.seedReader = bytes.NewReader(bytes.Repeat([]byte{0x01}, 32))
	second.seedReader = bytes.NewReader(bytes.Repeat([]byte{0x02}, 32))

	for i := int32(0); i < 3; i++ {
		if _, _, err := first.ProcessBlock(testBlock(i, uint32(i))); err != nil {
			t.Fatalf("first ProcessBlock(%d): %v", i, err)
		}
		if _, _, err := second.ProcessBlock(testBlock(i, uint32(i))); err != nil {
			t.Fatalf("second ProcessBlock(%d): %v", i, err)
		}
	}

	firstInfo, ok := first.EpochInfo(0)
	if !ok {
		t.Fatalf("first manager missing epoch 0")
	}
	secondInfo, ok := second.EpochInfo(0)
	if !ok {
		t.Fatalf("second manager missing epoch 0")
	}
	if firstInfo.EpochSeed == secondInfo.EpochSeed {
		t.Fatalf("independent managers generated the same encoding seed")
	}
}

func TestPersistentManagerReloadsEpochs(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "ffldb")
	db, err := database.Create("ffldb", dbPath, wire.SimNet)
	if err != nil {
		t.Fatalf("database.Create: %v", err)
	}
	defer db.Close()

	config := Config{
		K:       3,
		S:       2,
		C:       0.3,
		Delta:   0.05,
		Version: 1,
	}

	first, err := NewPersistent(db, config)
	if err != nil {
		t.Fatalf("NewPersistent first: %v", err)
	}
	for i := int32(0); i < 3; i++ {
		if _, _, err := first.ProcessBlock(testBlock(i, uint32(i))); err != nil {
			t.Fatalf("ProcessBlock(%d): %v", i, err)
		}
	}
	want, err := first.DropletsMessage(0, 0, 2)
	if err != nil {
		t.Fatalf("first DropletsMessage: %v", err)
	}
	wantInfo, ok := first.EpochInfo(0)
	if !ok {
		t.Fatalf("first manager missing epoch 0")
	}

	second, err := NewPersistent(db, config)
	if err != nil {
		t.Fatalf("NewPersistent second: %v", err)
	}
	info, ok := second.EpochInfo(0)
	if !ok {
		t.Fatalf("reloaded manager missing epoch 0")
	}
	if info.NumDroplets != 2 {
		t.Fatalf("reloaded num droplets = %d, want 2", info.NumDroplets)
	}
	if info.EpochSeed != wantInfo.EpochSeed {
		t.Fatalf("reloaded encoding seed differs")
	}

	got, err := second.DropletsMessage(0, 0, 2)
	if err != nil {
		t.Fatalf("second DropletsMessage: %v", err)
	}
	for i := range want.Droplets {
		wantDroplet := want.Droplets[i]
		gotDroplet := got.Droplets[i]
		if wantDroplet.DropletID != gotDroplet.DropletID ||
			wantDroplet.PaddedLen != gotDroplet.PaddedLen ||
			!bytes.Equal(wantDroplet.Payload, gotDroplet.Payload) {

			t.Fatalf("reloaded droplet %d differs", i)
		}
	}
}
