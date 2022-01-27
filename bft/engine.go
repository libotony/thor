package bft

import (
	"errors"
	"sort"

	lru "github.com/hashicorp/golang-lru"
	"github.com/vechain/thor/block"
	"github.com/vechain/thor/builtin"
	"github.com/vechain/thor/cache"
	"github.com/vechain/thor/chain"
	"github.com/vechain/thor/kv"
	"github.com/vechain/thor/muxdb"
	"github.com/vechain/thor/state"
	"github.com/vechain/thor/thor"
)

const storeName = "bft.engine"

var votedKey = []byte("packer-voted")

type GetBlockHeader func(id thor.Bytes32) (*block.Header, error)

type BFTEngine struct {
	repo       *chain.Repository
	store      kv.Store
	stater     *state.Stater
	forkConfig thor.ForkConfig
	voted      map[thor.Bytes32]uint32
	caches     struct {
		state   *lru.Cache
		weight  *lru.Cache
		mbp     *lru.Cache
		voteset *cache.PrioCache
	}
}

type BFTState struct {
	Weight    uint32
	Justified *thor.Bytes32
	Committed *thor.Bytes32
}

func NewEngine(repo *chain.Repository, mainDB *muxdb.MuxDB, forkConfig thor.ForkConfig) (*BFTEngine, error) {
	store := mainDB.NewStore(storeName)

	voted, err := loadVoted(store)
	if err != nil && !store.IsNotFound(err) {
		return nil, err
	}

	engine := BFTEngine{
		repo:       repo,
		store:      store,
		stater:     state.NewStater(mainDB),
		voted:      voted,
		forkConfig: forkConfig,
	}

	engine.caches.state, _ = lru.New(1024)
	engine.caches.weight, _ = lru.New(1024)
	engine.caches.mbp, _ = lru.New(8)
	engine.caches.voteset = cache.NewPrioCache(16)

	return &engine, nil
}

func (engine *BFTEngine) Process(header *block.Header) (becomeNewBest bool, newCommitted *thor.Bytes32, err error) {
	best := engine.repo.BestBlockSummary().Header
	if header.Number() < engine.forkConfig.FINALITY || best.Number() < engine.forkConfig.FINALITY {
		return header.BetterThan(best), nil, nil
	}

	committed := engine.repo.Committed()
	if !committed.IsZero() {
		if included, err := engine.repo.NewChain(header.ParentID()).HasBlock(committed); err != nil {
			return false, nil, err
		} else if !included {
			return false, nil, errConflictWithCommitted
		}
	}

	st, err := engine.getState(header.ID(), func(id thor.Bytes32) (*block.Header, error) {
		// header was not added to repo at this moment
		if id == header.ID() {
			return header, nil
		}
		return engine.getBlockHeader(id)
	})
	if err != nil {
		return false, nil, err
	}

	bSt, err := engine.getState(best.ID(), engine.getBlockHeader)
	if err != nil {
		return false, nil, err
	}

	if st.Weight != bSt.Weight {
		becomeNewBest = st.Weight > bSt.Weight
	} else {
		becomeNewBest = header.BetterThan(best)
	}

	if st.Committed != nil && header.ID() == *st.Committed {
		id, err := engine.findCheckpointByWeight(st.Weight-1, committed, header.ParentID())
		if err != nil {
			return false, nil, err
		}
		newCommitted = &id
	}

	return
}

func (engine *BFTEngine) AddVoted(parentID thor.Bytes32) error {
	checkpoint, err := engine.repo.NewChain(parentID).GetBlockID(block.Number(parentID) / thor.BFTRoundInterval * thor.BFTRoundInterval)
	if err != nil {
		return nil
	}

	st, err := engine.getState(parentID, engine.getBlockHeader)
	if err != nil {
		return err
	}

	engine.voted[checkpoint] = st.Weight
	return nil
}

func (engine *BFTEngine) GetVote(parentID thor.Bytes32) (block.Vote, error) {
	st, err := engine.getState(parentID, engine.getBlockHeader)
	if err != nil {
		return block.WIT, err
	}

	committed := engine.repo.Committed()
	var latestJustified thor.Bytes32
	weight := st.Weight
	if st.Justified != nil {
		checkpoint, err := engine.repo.NewChain(parentID).GetBlockID(block.Number(parentID) / thor.BFTRoundInterval * thor.BFTRoundInterval)
		if err != nil {
			return block.WIT, err
		}
		latestJustified = checkpoint
	} else {
		checkpoint, err := engine.findCheckpointByWeight(st.Weight, committed, parentID)
		if err != nil {
			return block.WIT, err
		}
		latestJustified = checkpoint
	}

	// see https://github.com/vechain/VIPs
	for k, v := range engine.voted {
		if block.Number(k) > block.Number(committed) {
			a, b := latestJustified, k
			if block.Number(k) > block.Number(latestJustified) {
				a, b = k, latestJustified
			}

			if includes, err := engine.repo.NewChain(a).HasBlock(b); err != nil {
				return block.WIT, nil
			} else if !includes && v >= weight-1 {
				return block.WIT, nil
			}
		}
	}

	return block.COM, nil
}

func (engine *BFTEngine) Close() {
	if len(engine.voted) > 0 {
		toSave := make(map[thor.Bytes32]uint32)
		committed := engine.repo.Committed()

		for k, v := range engine.voted {
			if block.Number(k) < block.Number(committed) {
				toSave[k] = v
			}
		}

		if len(toSave) > 0 {
			saveVoted(engine.store, toSave)
		}
	}
}

func (engine *BFTEngine) getBlockHeader(id thor.Bytes32) (*block.Header, error) {
	sum, err := engine.repo.GetBlockSummary(id)
	if err != nil {
		return nil, err
	}
	return sum.Header, nil
}

func (engine *BFTEngine) getState(blockID thor.Bytes32, getHeader GetBlockHeader) (*BFTState, error) {
	if cached, ok := engine.caches.state.Get(blockID); ok {
		return cached.(*BFTState), nil
	}

	var (
		vs  *voteSet
		end uint32
	)

	header, err := getHeader(blockID)
	if err != nil {
		return nil, err
	}

	if entry := engine.caches.voteset.Remove(header.ParentID()); entry != nil {
		vs = interface{}(entry).(*voteSet)
		end = block.Number(header.ParentID())
	} else {
		var err error
		vs, err = newVoteSet(engine, header.ParentID())
		if err != nil {
			return nil, err
		}
		end = vs.checkpoint
	}

	h := header
	for {
		if vs.isCommitted() {
			break
		}

		signer, err := h.Signer()
		if err != nil {
			return nil, err
		}
		vs.addVote(signer, h.IsComVote(), h.ID())

		if h.Number() <= end {
			break
		}

		h, err = getHeader(h.ParentID())
		if err != nil {
			return nil, err
		}
	}

	st := vs.getState()

	// save weight at the end of round
	if (header.Number()+1)%thor.BFTRoundInterval == 0 {
		if err := saveWeight(engine.store, header.ID(), st.Weight); err != nil {
			return nil, err
		}
		engine.caches.weight.Add(header.ID(), st.Weight)
	}

	engine.caches.state.Add(header.ID(), st)
	engine.caches.voteset.Set(header.ID(), vs, float64(header.Number()))
	return st, nil
}

func (engine *BFTEngine) findCheckpointByWeight(target uint32, committed, parentID thor.Bytes32) (blockID thor.Bytes32, err error) {
	defer func() {
		if e := recover(); e != nil {
			err = e.(error)
			return
		}
	}()

	searchStart := block.Number(committed)
	if searchStart == 0 {
		searchStart = engine.forkConfig.FINALITY / thor.BFTRoundInterval * thor.BFTRoundInterval
	}

	c := engine.repo.NewChain(parentID)
	get := func(i int) (uint32, error) {
		id, err := c.GetBlockID(searchStart + uint32(i+1)*thor.BFTRoundInterval - 1)
		if err != nil {
			return 0, err
		}
		return engine.getWeight(id)
	}

	n := int((block.Number(parentID) + 1 - searchStart) / thor.BFTRoundInterval)
	num := sort.Search(n, func(i int) bool {
		weight, err := get(i)
		if err != nil {
			panic(err)
		}

		return weight >= target
	})

	if num == n {
		return thor.Bytes32{}, errors.New("failed find the block by weight")
	}

	weight, err := get(num)
	if err != nil {
		return thor.Bytes32{}, err
	}

	if weight != target {
		return thor.Bytes32{}, errors.New("failed to find the block by weight")
	}

	return c.GetBlockID(searchStart + uint32(num)*thor.BFTRoundInterval)
}

func (engine *BFTEngine) getMaxBlockProposers(sum *chain.BlockSummary) (mbp uint64, err error) {
	if cached, ok := engine.caches.mbp.Get(sum.Header.ID()); ok {
		return cached.(uint64), nil
	}

	defer func() {
		if err != nil {
			engine.caches.mbp.Add(sum.Header.ID(), mbp)
		}
	}()

	state := engine.stater.NewState(sum.Header.StateRoot(), sum.Header.Number(), sum.Conflicts, sum.SteadyNum)
	params, err := builtin.Params.Native(state).Get(thor.KeyMaxBlockProposers)
	if err != nil {
		return
	}
	mbp = params.Uint64()
	if mbp == 0 || mbp > thor.InitialMaxBlockProposers {
		mbp = thor.InitialMaxBlockProposers
	}

	return
}

func (engine *BFTEngine) getWeight(id thor.Bytes32) (weight uint32, err error) {
	if cached, ok := engine.caches.weight.Get(id); ok {
		return cached.(uint32), nil
	}

	defer func() {
		if err != nil {
			engine.caches.weight.Add(id, weight)
		}
	}()

	return loadWeight(engine.store, id)
}
