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
	ErrUnknownPeer       = fmt.Errorf("unknown fountain bootstrap peer")
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
	peers map[int32]*peerEpochState
}

type peerEpochState struct {
	info    wire.FtnEpochInfo
	records map[uint64]wire.DropletRecord
}

type encodingRecords struct {
	info    wire.FtnEpochInfo
	records []wire.DropletRecord
}

type encodingDropletID struct {
	seed      [32]byte
	dropletID uint64
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

func (c *Collector) RegisterEpoch(peerID int32, info wire.FtnEpochInfo) error {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	state := c.epochs[info.EpochID]
	if state == nil {
		state = &epochState{peers: make(map[int32]*peerEpochState)}
		c.epochs[info.EpochID] = state
	}

	peerState := state.peers[peerID]
	if peerState == nil {
		state.peers[peerID] = &peerEpochState{
			info:    info,
			records: make(map[uint64]wire.DropletRecord),
		}
		return nil
	}

	if peerState.info.EpochSeed != info.EpochSeed {
		return fmt.Errorf("%w: epoch %d peer %d", ErrEpochSeedMismatch,
			info.EpochID, peerID)
	}
	peerState.info = info
	return nil
}

func (c *Collector) AddDroplets(epochID uint64, peerID int32,
	records []*wire.DropletRecord) (int, error) {

	c.mtx.Lock()
	defer c.mtx.Unlock()

	state := c.epochs[epochID]
	if state == nil {
		return 0, fmt.Errorf("%w: %d", ErrUnknownEpoch, epochID)
	}
	peerState := state.peers[peerID]
	if peerState == nil {
		return 0, fmt.Errorf("%w: epoch %d peer %d", ErrUnknownPeer,
			epochID, peerID)
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

		key := encodingDropletID{
			seed:      peerState.info.EpochSeed,
			dropletID: rec.DropletID,
		}
		if !hasEncodingDroplet(state, key) {
			added++
		}
		peerState.records[rec.DropletID] = copyDropletRecord(*rec)
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

	return uniqueDropletCount(state)
}

func (c *Collector) PeerDropletCount(epochID uint64, peerID int32) int {
	c.mtx.RLock()
	defer c.mtx.RUnlock()

	state := c.epochs[epochID]
	if state == nil || state.peers[peerID] == nil {
		return 0
	}
	return len(state.peers[peerID].records)
}

func (c *Collector) RemovePeer(epochID uint64, peerID int32) int {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	state := c.epochs[epochID]
	if state == nil {
		return 0
	}

	peerState := state.peers[peerID]
	if peerState == nil {
		return 0
	}

	before := uniqueDropletCount(state)
	delete(state.peers, peerID)
	if len(state.peers) == 0 {
		delete(c.epochs, epochID)
	}

	return before - uniqueDropletCount(state)
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

// DecodableEpochs returns the sorted IDs of collected epochs which have
// enough unique droplets to attempt a decode and have not yet been decoded.
func (c *Collector) DecodableEpochs() []uint64 {
	c.mtx.RLock()
	defer c.mtx.RUnlock()

	threshold := int(c.config.K)
	epochIDs := make([]uint64, 0, len(c.epochs))
	for epochID, state := range c.epochs {
		if _, decoded := c.decoded[epochID]; decoded ||
			uniqueDropletCount(state) < threshold {
			continue
		}
		epochIDs = append(epochIDs, epochID)
	}
	sort.Slice(epochIDs, func(i, j int) bool {
		return epochIDs[i] < epochIDs[j]
	})
	return epochIDs
}

func (c *Collector) Records(epochID uint64) ([]*wire.DropletRecord, error) {
	c.mtx.RLock()
	defer c.mtx.RUnlock()

	state, ok := c.epochs[epochID]
	if !ok {
		return nil, fmt.Errorf("%w: %d", ErrUnknownEpoch, epochID)
	}

	peerIDs := make([]int32, 0, len(state.peers))
	for peerID := range state.peers {
		peerIDs = append(peerIDs, peerID)
	}
	sort.Slice(peerIDs, func(i, j int) bool { return peerIDs[i] < peerIDs[j] })

	records := make([]*wire.DropletRecord, 0)
	seen := make(map[encodingDropletID]struct{})
	for _, peerID := range peerIDs {
		peerState := state.peers[peerID]
		ids := sortedDropletIDs(peerState.records)
		for _, dropletID := range ids {
			key := encodingDropletID{
				seed:      peerState.info.EpochSeed,
				dropletID: dropletID,
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			rec := copyDropletRecord(peerState.records[dropletID])
			records = append(records, &rec)
		}
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

	peerIDs := make([]int32, 0, len(state.peers))
	for peerID := range state.peers {
		peerIDs = append(peerIDs, peerID)
	}
	sort.Slice(peerIDs, func(i, j int) bool { return peerIDs[i] < peerIDs[j] })

	bySeed := make(map[[32]byte]*encodingRecords)
	for _, peerID := range peerIDs {
		peerState := state.peers[peerID]
		encoding := bySeed[peerState.info.EpochSeed]
		if encoding == nil {
			encoding = &encodingRecords{
				info: peerState.info,
			}
			bySeed[peerState.info.EpochSeed] = encoding
		}
		existing := make(map[uint64]struct{}, len(encoding.records))
		for _, rec := range encoding.records {
			existing[rec.DropletID] = struct{}{}
		}
		ids := sortedDropletIDs(peerState.records)
		for _, dropletID := range ids {
			if _, ok := existing[dropletID]; ok {
				continue
			}
			existing[dropletID] = struct{}{}
			encoding.records = append(encoding.records,
				copyDropletRecord(peerState.records[dropletID]))
		}
	}
	c.mtx.RUnlock()

	seeds := make([][32]byte, 0, len(bySeed))
	for seed := range bySeed {
		seeds = append(seeds, seed)
	}
	sort.Slice(seeds, func(i, j int) bool {
		return string(seeds[i][:]) < string(seeds[j][:])
	})

	var droplets []droplet.Droplet
	for _, seed := range seeds {
		encoding := bySeed[seed]
		peerDroplets, err := c.recordsForDecode(
			encoding.info, encoding.records,
		)
		if err != nil {
			return nil, err
		}
		droplets = append(droplets, peerDroplets...)
	}
	return droplets, nil
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

func sortedDropletIDs(records map[uint64]wire.DropletRecord) []uint64 {
	ids := make([]uint64, 0, len(records))
	for dropletID := range records {
		ids = append(ids, dropletID)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

func hasEncodingDroplet(state *epochState, key encodingDropletID) bool {
	for _, peerState := range state.peers {
		if peerState.info.EpochSeed != key.seed {
			continue
		}
		if _, ok := peerState.records[key.dropletID]; ok {
			return true
		}
	}
	return false
}

func uniqueDropletCount(state *epochState) int {
	if state == nil {
		return 0
	}

	unique := make(map[encodingDropletID]struct{})
	for _, peerState := range state.peers {
		for dropletID := range peerState.records {
			unique[encodingDropletID{
				seed:      peerState.info.EpochSeed,
				dropletID: dropletID,
			}] = struct{}{}
		}
	}
	return len(unique)
}
