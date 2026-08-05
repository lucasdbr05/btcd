package bootstrap

import (
	"fmt"
	"sort"
	"sync"

	"github.com/btcsuite/btcd/fountain/decoder"
	"github.com/btcsuite/btcd/fountain/distribution"
	"github.com/btcsuite/btcd/fountain/droplet"
	"github.com/btcsuite/btcd/wire"
)

var (
	ErrUnknownEpoch      = fmt.Errorf("unknown fountain bootstrap epoch")
	ErrEpochSeedMismatch = fmt.Errorf("fountain bootstrap epoch seed mismatch")
)

type Config struct {
	K     uint32
	C     float64
	Delta float64
}

type Collector struct {
	mtx     sync.RWMutex
	config  Config
	epochs  map[uint64]*epochState
	decoded map[uint64]struct{}
}

type epochState struct {
	info    wire.FtnEpochInfo
	records map[uint64]wire.DropletRecord
	sources map[uint64]map[int32]struct{}
}

func NewCollector(config Config) *Collector {
	return &Collector{
		config:  config,
		epochs:  make(map[uint64]*epochState),
		decoded: make(map[uint64]struct{}),
	}
}

func (c *Collector) DecodeThreshold() int {
	return int(c.config.K)
}

func (c *Collector) RegisterEpoch(info wire.FtnEpochInfo) error {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	state, ok := c.epochs[info.EpochID]
	if !ok {
		c.epochs[info.EpochID] = &epochState{
			info:    info,
			records: make(map[uint64]wire.DropletRecord),
			sources: make(map[uint64]map[int32]struct{}),
		}
		return nil
	}

	if state.info.EpochSeed != info.EpochSeed {
		return fmt.Errorf("%w: %d", ErrEpochSeedMismatch, info.EpochID)
	}
	state.info = info
	return nil
}

func (c *Collector) AddDroplets(epochID uint64, peerID int32,
	records []*wire.DropletRecord) (int, error) {

	c.mtx.Lock()
	defer c.mtx.Unlock()

	state, ok := c.epochs[epochID]
	if !ok {
		return 0, fmt.Errorf("%w: %d", ErrUnknownEpoch, epochID)
	}

	added := 0
	for _, rec := range records {
		if rec == nil {
			continue
		}
		if uint32(len(rec.Payload)) != rec.PaddedLen {
			return added, fmt.Errorf("droplet payload length %d != padded_len %d",
				len(rec.Payload), rec.PaddedLen)
		}

		if _, exists := state.records[rec.DropletID]; !exists {
			added++
		}
		state.records[rec.DropletID] = copyDropletRecord(*rec)

		sources := state.sources[rec.DropletID]
		if sources == nil {
			sources = make(map[int32]struct{})
			state.sources[rec.DropletID] = sources
		}
		sources[peerID] = struct{}{}
	}

	return added, nil
}

func (c *Collector) DropletCount(epochID uint64) int {
	c.mtx.RLock()
	defer c.mtx.RUnlock()

	state := c.epochs[epochID]
	if state == nil {
		return 0
	}
	return len(state.records)
}

func (c *Collector) RemovePeer(epochID uint64, peerID int32) int {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	state := c.epochs[epochID]
	if state == nil {
		return 0
	}

	removed := 0
	for dropletID, sources := range state.sources {
		if _, ok := sources[peerID]; !ok {
			continue
		}

		delete(state.records, dropletID)
		delete(state.sources, dropletID)
		removed++
	}

	return removed
}

func (c *Collector) MarkDecoded(epochID uint64) {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	c.decoded[epochID] = struct{}{}
}

func (c *Collector) Decoded(epochID uint64) bool {
	c.mtx.RLock()
	defer c.mtx.RUnlock()

	_, ok := c.decoded[epochID]
	return ok
}

func (c *Collector) Records(epochID uint64) ([]*wire.DropletRecord, error) {
	c.mtx.RLock()
	defer c.mtx.RUnlock()

	state, ok := c.epochs[epochID]
	if !ok {
		return nil, fmt.Errorf("%w: %d", ErrUnknownEpoch, epochID)
	}

	ids := make([]uint64, 0, len(state.records))
	for dropletID := range state.records {
		ids = append(ids, dropletID)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

	records := make([]*wire.DropletRecord, 0, len(ids))
	for _, dropletID := range ids {
		rec := copyDropletRecord(state.records[dropletID])
		records = append(records, &rec)
	}

	return records, nil
}

func (c *Collector) DropletsForDecode(epochID uint64) ([]droplet.Droplet, error) {
	c.mtx.RLock()
	state, ok := c.epochs[epochID]
	if !ok {
		c.mtx.RUnlock()
		return nil, fmt.Errorf("%w: %d", ErrUnknownEpoch, epochID)
	}

	info := state.info
	records := make([]wire.DropletRecord, 0, len(state.records))
	for _, rec := range state.records {
		records = append(records, copyDropletRecord(rec))
	}
	c.mtx.RUnlock()

	return c.recordsForDecode(info, records)
}

func (c *Collector) DecodeEpoch(epochID uint64,
	verifier decoder.BlockVerifier) (decoder.DecodeResult, error) {

	droplets, err := c.DropletsForDecode(epochID)
	if err != nil {
		return decoder.DecodeResult{}, err
	}

	return decoder.PeelingDecode(int(c.config.K), droplets, verifier), nil
}

func (c *Collector) recordsForDecode(info wire.FtnEpochInfo,
	records []wire.DropletRecord) ([]droplet.Droplet, error) {

	params := droplet.NewEpochParams(info.EpochID, c.config.K, info.EpochSeed)
	dist := distribution.NewRobustSoliton(
		int(c.config.K), c.config.C, c.config.Delta,
	)

	droplets := make([]droplet.Droplet, 0, len(records))
	for _, rec := range records {
		d := droplet.Droplet{
			EpochID:   info.EpochID,
			DropletID: rec.DropletID,
			Indices:   params.DeriveIndices(dist, rec.DropletID),
			PaddedLen: rec.PaddedLen,
			Payload:   append([]byte(nil), rec.Payload...),
		}
		if err := d.Validate(c.config.K); err != nil {
			return nil, err
		}
		droplets = append(droplets, d)
	}

	return droplets, nil
}

func copyDropletRecord(rec wire.DropletRecord) wire.DropletRecord {
	return wire.DropletRecord{
		DropletID: rec.DropletID,
		PaddedLen: rec.PaddedLen,
		Payload:   append([]byte(nil), rec.Payload...),
	}
}
