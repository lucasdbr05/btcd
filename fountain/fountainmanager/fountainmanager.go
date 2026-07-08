package manager

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/database"
	"github.com/btcsuite/btcd/fountain/distribution"
	"github.com/btcsuite/btcd/fountain/droplet"
	"github.com/btcsuite/btcd/fountain/epoch"
	"github.com/btcsuite/btcd/wire"
)

const (
	DefaultK       uint32  = 1000
	DefaultS       uint64  = 10
	DefaultC       float64 = 0.3
	DefaultDelta   float64 = 0.05
	DefaultVersion uint8   = 1
)

var (
	rootBucketKey     = []byte("fountain")
	epochsBucketKey   = []byte("epochs")
	infoKey           = []byte("info")
	dropletsBucketKey = []byte("droplets")
)

var (
	ErrInvalidConfig = errors.New("invalid fountain manager config")
	ErrUnknownEpoch  = errors.New("unknown fountain epoch")
)

type Config struct {
	K       uint32
	S       uint64
	C       float64
	Delta   float64
	Version uint8
}

type Manager struct {
	mtx     sync.RWMutex
	config  Config
	db      database.DB
	epochs  map[uint64]*epochState
	pending *pendingEpoch
}

type epochState struct {
	info     wire.FtnEpochInfo
	droplets map[uint64]wire.DropletRecord
}

type pendingEpoch struct {
	epochID        uint64
	firstBlockHash string
	blocks         [][]byte
}

func DefaultConfig() Config {
	return Config{
		K:       DefaultK,
		S:       DefaultS,
		C:       DefaultC,
		Delta:   DefaultDelta,
		Version: DefaultVersion,
	}
}

func New(config Config) (*Manager, error) {
	if err := validateConfig(config); err != nil {
		return nil, err
	}

	return &Manager{
		config: config,
		epochs: make(map[uint64]*epochState),
	}, nil
}

func NewPersistent(db database.DB, config Config) (*Manager, error) {
	if db == nil {
		return nil, errors.New("nil database")
	}

	mgr, err := New(config)
	if err != nil {
		return nil, err
	}
	mgr.db = db

	if err := mgr.loadFromDB(); err != nil {
		return nil, err
	}

	return mgr, nil
}

func NewDefault() *Manager {
	mgr, err := New(DefaultConfig())
	if err != nil {
		panic(err)
	}
	return mgr
}

func (m *Manager) Config() Config {
	m.mtx.RLock()
	config := m.config
	m.mtx.RUnlock()
	return config
}

func (m *Manager) RegisterEpoch(info wire.FtnEpochInfo) error {
	state, err := m.registerEpochState(info)
	if err != nil {
		return err
	}

	if err := m.persistEpochState(state); err != nil {
		return err
	}

	m.mtx.Lock()
	defer m.mtx.Unlock()

	existing, ok := m.epochs[info.EpochID]
	if !ok {
		m.epochs[info.EpochID] = state
		return nil
	}

	existing.info = info
	existing.info.NumDroplets = uint64(len(existing.droplets))
	return nil
}

func (m *Manager) EpochInfo(epochID uint64) (wire.FtnEpochInfo, bool) {
	m.mtx.RLock()
	defer m.mtx.RUnlock()

	state, ok := m.epochs[epochID]
	if !ok {
		return wire.FtnEpochInfo{}, false
	}
	return state.info, true
}

func (m *Manager) AvailableEpochs() []*wire.FtnEpochInfo {
	m.mtx.RLock()
	defer m.mtx.RUnlock()

	ids := make([]uint64, 0, len(m.epochs))
	for epochID := range m.epochs {
		ids = append(ids, epochID)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

	epochs := make([]*wire.FtnEpochInfo, 0, len(ids))
	for _, epochID := range ids {
		info := m.epochs[epochID].info
		epochs = append(epochs, &info)
	}

	return epochs
}

func (m *Manager) FtnEpochsMessage() *wire.MsgFtnEpochs {
	msg := wire.NewMsgFtnEpochs()
	msg.Epochs = m.AvailableEpochs()
	return msg
}

func (m *Manager) ProcessBlock(block *btcutil.Block) (wire.FtnEpochInfo, bool, error) {
	if block == nil {
		return wire.FtnEpochInfo{}, false, errors.New("nil block")
	}

	height := block.Height()
	if height < 0 {
		return wire.FtnEpochInfo{}, false, fmt.Errorf(
			"block %s has unknown height", block.Hash(),
		)
	}

	var serialized bytes.Buffer
	if err := block.MsgBlock().Serialize(&serialized); err != nil {
		return wire.FtnEpochInfo{}, false, err
	}

	m.mtx.Lock()
	k := m.config.K
	epochID := uint64(height) / uint64(k)
	offset := int(uint64(height) % uint64(k))

	if m.pending == nil || m.pending.epochID != epochID {
		if offset != 0 {
			m.mtx.Unlock()
			return wire.FtnEpochInfo{}, false, nil
		}
		m.pending = &pendingEpoch{
			epochID:        epochID,
			firstBlockHash: block.Hash().String(),
			blocks:         make([][]byte, 0, k),
		}
	}

	if offset != len(m.pending.blocks) {
		if offset == 0 {
			m.pending = &pendingEpoch{
				epochID:        epochID,
				firstBlockHash: block.Hash().String(),
				blocks:         make([][]byte, 0, k),
			}
		} else {
			m.mtx.Unlock()
			return wire.FtnEpochInfo{}, false, nil
		}
	}

	blockBytes := append([]byte(nil), serialized.Bytes()...)
	m.pending.blocks = append(m.pending.blocks, blockBytes)
	if len(m.pending.blocks) < int(k) {
		m.mtx.Unlock()
		return wire.FtnEpochInfo{}, false, nil
	}

	completedEpoch := m.pending
	m.pending = nil
	config := m.config
	m.mtx.Unlock()

	state := buildEpochState(config, completedEpoch)
	if err := m.persistEpochState(state); err != nil {
		return wire.FtnEpochInfo{}, false, err
	}

	m.mtx.Lock()
	m.epochs[state.info.EpochID] = state
	m.mtx.Unlock()

	return state.info, true, nil
}

func (m *Manager) PutDroplet(epochID uint64, rec wire.DropletRecord) error {
	if uint32(len(rec.Payload)) != rec.PaddedLen {
		return fmt.Errorf("droplet payload length %d != padded_len %d",
			len(rec.Payload), rec.PaddedLen)
	}

	m.mtx.Lock()
	state, ok := m.epochs[epochID]
	if !ok {
		m.mtx.Unlock()
		return fmt.Errorf("%w: %d", ErrUnknownEpoch, epochID)
	}

	recCopy := copyDropletRecord(rec)
	nextInfo := state.info
	if _, exists := state.droplets[rec.DropletID]; !exists {
		nextInfo.NumDroplets++
	}
	m.mtx.Unlock()

	if err := m.persistDroplet(epochID, nextInfo, recCopy); err != nil {
		return err
	}

	m.mtx.Lock()
	defer m.mtx.Unlock()
	state, ok = m.epochs[epochID]
	if !ok {
		return fmt.Errorf("%w: %d", ErrUnknownEpoch, epochID)
	}
	state.droplets[rec.DropletID] = recCopy
	state.info.NumDroplets = uint64(len(state.droplets))
	return nil
}

func (m *Manager) PutDroplets(epochID uint64, records []*wire.DropletRecord) error {
	for _, rec := range records {
		if rec == nil {
			continue
		}
		if err := m.PutDroplet(epochID, *rec); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) GetDroplets(epochID, startID uint64,
	count uint64) ([]*wire.DropletRecord, error) {

	if count == 0 || count > wire.MaxDropletsPerRequest {
		return nil, fmt.Errorf("droplet count out of range [%d, max %d]",
			count, wire.MaxDropletsPerRequest)
	}

	m.mtx.RLock()
	defer m.mtx.RUnlock()

	state, ok := m.epochs[epochID]
	if !ok {
		return nil, fmt.Errorf("%w: %d", ErrUnknownEpoch, epochID)
	}

	endID := startID + uint64(count)
	if endID < startID {
		endID = ^uint64(0)
	}

	ids := make([]uint64, 0, len(state.droplets))
	for dropletID := range state.droplets {
		if dropletID >= startID && dropletID < endID {
			ids = append(ids, dropletID)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

	records := make([]*wire.DropletRecord, 0, len(ids))
	for _, dropletID := range ids {
		rec := copyDropletRecord(state.droplets[dropletID])
		records = append(records, &rec)
	}

	return records, nil
}

func (m *Manager) DropletsMessage(epochID, startID uint64,
	count uint64) (*wire.MsgDroplets, error) {

	records, err := m.GetDroplets(epochID, startID, count)
	if err != nil {
		return nil, err
	}

	msg := wire.NewMsgDroplets(epochID)
	msg.Droplets = records
	return msg, nil
}

func buildEpochState(config Config, completedEpoch *pendingEpoch) *epochState {
	params := droplet.NewEpochParams(
		completedEpoch.epochID, config.K,
		epoch.ComputeEpochSeed(
			int(completedEpoch.epochID), completedEpoch.firstBlockHash,
		),
	)
	dist := distribution.NewRobustSoliton(
		int(config.K), config.C, config.Delta,
	)
	encoder := droplet.NewEncoder(&params, dist, completedEpoch.blocks)
	generated := encoder.GenerateN(config.S)

	state := &epochState{
		info: wire.FtnEpochInfo{
			EpochID:     completedEpoch.epochID,
			EpochSeed:   params.EpochSeed,
			NumDroplets: uint64(len(generated)),
		},
		droplets: make(map[uint64]wire.DropletRecord, len(generated)),
	}
	for _, d := range generated {
		state.droplets[d.DropletID] = wire.DropletRecord{
			DropletID: d.DropletID,
			PaddedLen: d.PaddedLen,
			Payload:   append([]byte(nil), d.Payload...),
		}
	}

	return state
}

func (m *Manager) registerEpochState(info wire.FtnEpochInfo) (*epochState, error) {
	m.mtx.RLock()
	defer m.mtx.RUnlock()

	state, ok := m.epochs[info.EpochID]
	if !ok {
		return &epochState{
			info:     info,
			droplets: make(map[uint64]wire.DropletRecord),
		}, nil
	}

	stateCopy := copyEpochState(state)
	stateCopy.info = info
	stateCopy.info.NumDroplets = uint64(len(stateCopy.droplets))
	return stateCopy, nil
}

func (m *Manager) persistEpochState(state *epochState) error {
	if m.db == nil {
		return nil
	}

	state = copyEpochState(state)
	return m.db.Update(func(tx database.Tx) error {
		epochBucket, err := writableEpochBucket(tx, state.info.EpochID)
		if err != nil {
			return err
		}
		if err := epochBucket.Put(infoKey, serializeEpochInfo(state.info)); err != nil {
			return err
		}

		if epochBucket.Bucket(dropletsBucketKey) != nil {
			if err := epochBucket.DeleteBucket(dropletsBucketKey); err != nil {
				return err
			}
		}
		dropletsBucket, err := epochBucket.CreateBucketIfNotExists(dropletsBucketKey)
		if err != nil {
			return err
		}
		for _, rec := range state.droplets {
			if err := dropletsBucket.Put(
				uint64Key(rec.DropletID), serializeDropletRecord(rec),
			); err != nil {
				return err
			}
		}

		return nil
	})
}

func (m *Manager) persistDroplet(epochID uint64, info wire.FtnEpochInfo,
	rec wire.DropletRecord) error {

	if m.db == nil {
		return nil
	}

	return m.db.Update(func(tx database.Tx) error {
		epochBucket, err := writableEpochBucket(tx, epochID)
		if err != nil {
			return err
		}
		if err := epochBucket.Put(infoKey, serializeEpochInfo(info)); err != nil {
			return err
		}

		dropletsBucket, err := epochBucket.CreateBucketIfNotExists(dropletsBucketKey)
		if err != nil {
			return err
		}
		return dropletsBucket.Put(
			uint64Key(rec.DropletID), serializeDropletRecord(rec),
		)
	})
}

func (m *Manager) loadFromDB() error {
	if m.db == nil {
		return nil
	}

	loaded := make(map[uint64]*epochState)
	if err := m.db.View(func(tx database.Tx) error {
		root := tx.Metadata().Bucket(rootBucketKey)
		if root == nil {
			return nil
		}

		epochsBucket := root.Bucket(epochsBucketKey)
		if epochsBucket == nil {
			return nil
		}

		return epochsBucket.ForEachBucket(func(epochKey []byte) error {
			if len(epochKey) != 8 {
				return nil
			}
			epochID := binary.BigEndian.Uint64(epochKey)
			epochBucket := epochsBucket.Bucket(epochKey)
			if epochBucket == nil {
				return nil
			}

			infoBytes := epochBucket.Get(infoKey)
			if infoBytes == nil {
				return nil
			}
			info, err := deserializeEpochInfo(infoBytes)
			if err != nil {
				return err
			}
			info.EpochID = epochID

			state := &epochState{
				info:     info,
				droplets: make(map[uint64]wire.DropletRecord),
			}
			dropletsBucket := epochBucket.Bucket(dropletsBucketKey)
			if dropletsBucket != nil {
				if err := dropletsBucket.ForEach(func(k, v []byte) error {
					if len(k) != 8 {
						return nil
					}
					dropletID := binary.BigEndian.Uint64(k)
					rec, err := deserializeDropletRecord(dropletID, v)
					if err != nil {
						return err
					}
					state.droplets[dropletID] = rec
					return nil
				}); err != nil {
					return err
				}
			}
			state.info.NumDroplets = uint64(len(state.droplets))
			loaded[epochID] = state
			return nil
		})
	}); err != nil {
		return err
	}

	m.mtx.Lock()
	m.epochs = loaded
	m.mtx.Unlock()

	return nil
}

func writableEpochBucket(tx database.Tx, epochID uint64) (database.Bucket, error) {
	root, err := tx.Metadata().CreateBucketIfNotExists(rootBucketKey)
	if err != nil {
		return nil, err
	}
	epochsBucket, err := root.CreateBucketIfNotExists(epochsBucketKey)
	if err != nil {
		return nil, err
	}
	return epochsBucket.CreateBucketIfNotExists(uint64Key(epochID))
}

func serializeEpochInfo(info wire.FtnEpochInfo) []byte {
	var buf [48]byte
	binary.BigEndian.PutUint64(buf[0:8], info.EpochID)
	copy(buf[8:40], info.EpochSeed[:])
	binary.BigEndian.PutUint64(buf[40:48], info.NumDroplets)
	return buf[:]
}

func deserializeEpochInfo(data []byte) (wire.FtnEpochInfo, error) {
	if len(data) != 48 {
		return wire.FtnEpochInfo{}, fmt.Errorf(
			"invalid fountain epoch info length %d", len(data),
		)
	}

	var info wire.FtnEpochInfo
	info.EpochID = binary.BigEndian.Uint64(data[0:8])
	copy(info.EpochSeed[:], data[8:40])
	info.NumDroplets = binary.BigEndian.Uint64(data[40:48])
	return info, nil
}

func serializeDropletRecord(rec wire.DropletRecord) []byte {
	data := make([]byte, 4+len(rec.Payload))
	binary.BigEndian.PutUint32(data[0:4], rec.PaddedLen)
	copy(data[4:], rec.Payload)
	return data
}

func deserializeDropletRecord(dropletID uint64, data []byte) (wire.DropletRecord, error) {
	if len(data) < 4 {
		return wire.DropletRecord{}, fmt.Errorf(
			"invalid fountain droplet record length %d", len(data),
		)
	}

	paddedLen := binary.BigEndian.Uint32(data[0:4])
	payload := append([]byte(nil), data[4:]...)
	if uint32(len(payload)) != paddedLen {
		return wire.DropletRecord{}, fmt.Errorf(
			"droplet payload length %d != padded_len %d",
			len(payload), paddedLen,
		)
	}

	return wire.DropletRecord{
		DropletID: dropletID,
		PaddedLen: paddedLen,
		Payload:   payload,
	}, nil
}

func uint64Key(v uint64) []byte {
	var key [8]byte
	binary.BigEndian.PutUint64(key[:], v)
	return key[:]
}

func copyEpochState(state *epochState) *epochState {
	stateCopy := &epochState{
		info:     state.info,
		droplets: make(map[uint64]wire.DropletRecord, len(state.droplets)),
	}
	for dropletID, rec := range state.droplets {
		stateCopy.droplets[dropletID] = copyDropletRecord(rec)
	}
	return stateCopy
}

func validateConfig(config Config) error {
	switch {
	case config.K == 0:
		return fmt.Errorf("%w: k must be > 0", ErrInvalidConfig)
	case config.S == 0:
		return fmt.Errorf("%w: s must be > 0", ErrInvalidConfig)
	case config.C <= 0:
		return fmt.Errorf("%w: c must be > 0", ErrInvalidConfig)
	case config.Delta <= 0 || config.Delta >= 1:
		return fmt.Errorf("%w: delta must be in (0, 1)", ErrInvalidConfig)
	case config.Version == 0:
		return fmt.Errorf("%w: version must be > 0", ErrInvalidConfig)
	default:
		return nil
	}
}

func copyDropletRecord(rec wire.DropletRecord) wire.DropletRecord {
	payload := append([]byte(nil), rec.Payload...)
	return wire.DropletRecord{
		DropletID: rec.DropletID,
		PaddedLen: rec.PaddedLen,
		Payload:   payload,
	}
}
