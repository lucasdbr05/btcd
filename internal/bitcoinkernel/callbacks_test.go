package bitcoinkernel

import "testing"

func TestCallbackStateCorrelatesBlockResult(t *testing.T) {
	t.Parallel()

	state := newCallbackState()
	hash := [32]byte{0x01}
	resultReady := state.expectBlock(hash)
	state.recordBlockResult(BlockResult{
		Hash:   hash,
		Mode:   ValidationInvalid,
		Reason: BlockResultConsensus,
	})

	select {
	case result := <-resultReady:
		if !result.Checked || result.Hash != hash ||
			result.Mode != ValidationInvalid ||
			result.Reason != BlockResultConsensus {

			t.Fatalf("unexpected correlated result: %+v", result)
		}
	default:
		t.Fatal("block result was not correlated")
	}

	state.stopExpectingBlock(hash)
	state.mu.Lock()
	_, pending := state.pendingBlocks[hash]
	state.mu.Unlock()
	if pending {
		t.Fatal("block result correlation was not removed")
	}
}
