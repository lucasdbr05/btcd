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

func TestCallbackStateDrainsEvents(t *testing.T) {
	t.Parallel()

	state := newCallbackState()
	tip := ChainEvent{
		Type:   BlockTipUpdated,
		Hash:   [32]byte{0x02},
		Height: 42,
	}
	state.recordChainEvent(ChainEvent{
		Type:   BlockConnected,
		Hash:   [32]byte{0x01},
		Height: 42,
	})
	state.recordChainEvent(tip)
	state.recordFlushError("flush failed")
	state.recordFatalError("fatal error")

	events := state.drainChainEvents()
	if len(events) != 2 || events[0].Type != BlockConnected ||
		events[1] != tip {

		t.Fatalf("unexpected chain events: %+v", events)
	}
	if events := state.drainChainEvents(); len(events) != 0 {
		t.Fatalf("events were not drained: %+v", events)
	}

	gotTip, ok := state.bestTip()
	if !ok || gotTip != tip {
		t.Fatalf("unexpected best tip: ok=%v tip=%+v", ok, gotTip)
	}

	errors := state.drainErrorEvents()
	if len(errors) != 2 || errors[0].Fatal || errors[0].Message != "flush failed" ||
		!errors[1].Fatal || errors[1].Message != "fatal error" {

		t.Fatalf("unexpected errors: %+v", errors)
	}
	if errors := state.drainErrorEvents(); len(errors) != 0 {
		t.Fatalf("errors were not drained: %+v", errors)
	}
}
