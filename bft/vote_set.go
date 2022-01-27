package bft

import (
	"github.com/vechain/thor/block"
	"github.com/vechain/thor/thor"
)

type voteSet struct {
	parentWeight uint32
	checkpoint   uint32
	threshold    uint64

	votes     map[thor.Address]bool
	comVotes  uint64
	justified *thor.Bytes32
	committed *thor.Bytes32
}

func newVoteSet(engine *BFTEngine, parentID thor.Bytes32) (*voteSet, error) {
	var parentWeight uint32 // parent round bft weight

	blockNum := block.Number(parentID) + 1
	absRound := blockNum/thor.BFTRoundInterval - engine.forkConfig.FINALITY/thor.BFTRoundInterval
	checkpoint := blockNum / thor.BFTRoundInterval * thor.BFTRoundInterval

	var lastOfParentRound uint32
	if checkpoint > 0 {
		lastOfParentRound = checkpoint - 1
	} else {
		lastOfParentRound = 0
	}
	sum, err := engine.repo.NewChain(parentID).GetBlockSummary(lastOfParentRound)
	if err != nil {
		return nil, err
	}
	mbp, err := engine.getMaxBlockProposers(sum)
	if err != nil {
		return nil, err
	}
	threshold := mbp * 2 / 3

	if absRound == 0 {
		parentWeight = 0
	} else {
		var err error
		parentWeight, err = engine.getWeight(sum.Header.ID())
		if err != nil {
			return nil, err
		}
	}

	return &voteSet{
		votes:        make(map[thor.Address]bool),
		parentWeight: parentWeight,
		checkpoint:   checkpoint,
		threshold:    threshold,
	}, nil
}

func (vs *voteSet) isCommitted() bool {
	return vs.committed != nil
}

func (vs *voteSet) addVote(signer thor.Address, isCom bool, blockID thor.Bytes32) {
	if vs.isCommitted() {
		return
	}

	if ok, votedCom := vs.votes[signer]; !ok {
		vs.votes[signer] = isCom
		if isCom {
			vs.comVotes++
		}
	} else if !votedCom && isCom {
		vs.votes[signer] = true
		vs.comVotes++
	}

	if vs.justified == nil && len(vs.votes) > int(vs.threshold) {
		vs.justified = &blockID
	}

	if vs.committed == nil && vs.comVotes > vs.threshold {
		vs.committed = &blockID
	}
}

func (vs *voteSet) getState() *BFTState {
	weight := vs.parentWeight
	if vs.justified != nil {
		weight = weight + 1
	}

	return &BFTState{
		Weight:    weight,
		Justified: vs.justified,
		Committed: vs.committed,
	}
}
