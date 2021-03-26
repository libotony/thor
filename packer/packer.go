// Copyright (c) 2018 The VeChainThor developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package packer

import (
	"errors"

	"github.com/vechain/thor/block"
	"github.com/vechain/thor/builtin"
	"github.com/vechain/thor/chain"
	"github.com/vechain/thor/poa"
	"github.com/vechain/thor/runtime"
	"github.com/vechain/thor/state"
	"github.com/vechain/thor/thor"
	"github.com/vechain/thor/tx"
	"github.com/vechain/thor/xenv"
)

// Packer to pack txs and build new blocks.
type Packer struct {
	repo           *chain.Repository
	stater         *state.Stater
	masters        []thor.Address
	nodeMaster     thor.Address
	beneficiary    *thor.Address
	targetGasLimit uint64
	forkConfig     thor.ForkConfig
	seeder         *poa.Seeder
}

// New create a new Packer instance.
// The beneficiary is optional, it defaults to endorsor if not set.
func New(
	repo *chain.Repository,
	stater *state.Stater,
	masters []thor.Address,
	beneficiary *thor.Address,
	forkConfig thor.ForkConfig) *Packer {

	return &Packer{
		repo,
		stater,
		masters,
		thor.Address{},
		beneficiary,
		0,
		forkConfig,
		poa.NewSeeder(repo),
	}
}

// Schedule schedule a packing flow to pack new block upon given parent and clock time.
func (p *Packer) Schedule(parent *block.Block, nowTimestamp uint64) (flow *Flow, err error) {
	state := p.stater.NewState(parent.Header().StateRoot())

	// Before process hook of VIP-191, update builtin extension contract's code to V2
	vip191 := p.forkConfig.VIP191
	if vip191 == 0 {
		vip191 = 1
	}
	if parent.Header().Number()+1 == vip191 {
		if err := state.SetCode(builtin.Extension.Address, builtin.Extension.V2.RuntimeBytecodes()); err != nil {
			return nil, err
		}
	}

	var features tx.Features
	if parent.Header().Number()+1 >= vip191 {
		features |= tx.DelegationFeature
	}

	authority := builtin.Authority.Native(state)
	endorsement, err := builtin.Params.Native(state).Get(thor.KeyProposerEndorsement)
	if err != nil {
		return nil, err
	}

	mbp, err := builtin.Params.Native(state).Get(thor.KeyMaxBlockProposers)
	if err != nil {
		return nil, err
	}
	maxBlockProposers := mbp.Uint64()
	if maxBlockProposers == 0 {
		maxBlockProposers = thor.InitialMaxBlockProposers
	}

	candidates, err := authority.Candidates(endorsement, maxBlockProposers)
	if err != nil {
		return nil, err
	}
	var (
		proposers   = make([]poa.Proposer, 0, len(candidates))
		beneficiary thor.Address
	)
	if p.beneficiary != nil {
		beneficiary = *p.beneficiary
	}

	for _, c := range candidates {
		proposers = append(proposers, poa.Proposer{
			Address: c.NodeMaster,
			Active:  c.Active,
		})
	}

	var sched poa.Scheduler
	var seed thor.Bytes32
	var newBlockTime uint64
	if parent.Header().Number()+1 >= p.forkConfig.VIP193 {
		seed, err = p.seeder.Generate(parent.Header().ID())
		if err != nil {
			return nil, err
		}
		var schedV2 *poa.SchedulerV2
		for _, master := range p.masters {
			if schedV2 == nil {
				schedV2, err = poa.NewSchedulerV2(master, proposers, parent, seed.Bytes())
			} else {
				err = schedV2.ChangeProposer(master)
			}
			if err != nil {
				continue
			}

			ts := schedV2.Schedule(nowTimestamp)
			if newBlockTime == 0 || ts < newBlockTime {
				newBlockTime = ts
				sched = schedV2
				p.nodeMaster = master
			}
		}
	} else {
		for _, master := range p.masters {
			schedV1, err := poa.NewSchedulerV1(master, proposers, parent.Header().Number(), parent.Header().Timestamp())
			if err != nil {
				continue
			}
			ts := schedV1.Schedule(nowTimestamp)
			if newBlockTime == 0 || ts < newBlockTime {
				newBlockTime = ts
				sched = schedV1
				p.nodeMaster = master
			}
		}
	}
	if sched == nil {
		return nil, errors.New("failed to schedule")
	}

	if p.beneficiary == nil {
		// no beneficiary not set, set it to endorsor
		for _, c := range candidates {
			if c.NodeMaster == p.nodeMaster {
				beneficiary = c.Endorsor
				break
			}
		}
	}

	updates, score := sched.Updates(newBlockTime)
	for _, u := range updates {
		if _, err := authority.Update(u.Address, u.Active); err != nil {
			return nil, err
		}
	}

	rt := runtime.New(
		p.repo.NewChain(parent.Header().ID()),
		state,
		&xenv.BlockContext{
			Beneficiary: beneficiary,
			Signer:      p.nodeMaster,
			Number:      parent.Header().Number() + 1,
			Time:        newBlockTime,
			GasLimit:    p.gasLimit(parent.Header().GasLimit()),
			TotalScore:  parent.Header().TotalScore() + score,
		},
		p.forkConfig)

	return newFlow(p, parent.Header(), rt, features, proposers, maxBlockProposers, seed.Bytes()), nil
}

// Mock create a packing flow upon given parent, but with a designated timestamp.
// It will skip the PoA verification and scheduling, and the block produced by
// the returned flow is not in consensus.
func (p *Packer) Mock(parent *block.Header, targetTime uint64, gasLimit uint64) (*Flow, error) {
	state := p.stater.NewState(parent.StateRoot())
	p.nodeMaster = p.masters[0]

	// Before process hook of VIP-191, update builtin extension contract's code to V2
	vip191 := p.forkConfig.VIP191
	if vip191 == 0 {
		vip191 = 1
	}

	if parent.Number()+1 == vip191 {
		if err := state.SetCode(builtin.Extension.Address, builtin.Extension.V2.RuntimeBytecodes()); err != nil {
			return nil, err
		}
	}

	var features tx.Features
	if parent.Number()+1 >= vip191 {
		features |= tx.DelegationFeature
	}

	gl := gasLimit
	if gasLimit == 0 {
		gl = p.gasLimit(parent.GasLimit())
	}

	rt := runtime.New(
		p.repo.NewChain(parent.ID()),
		state,
		&xenv.BlockContext{
			Beneficiary: p.nodeMaster,
			Signer:      p.nodeMaster,
			Number:      parent.Number() + 1,
			Time:        targetTime,
			GasLimit:    gl,
			TotalScore:  parent.TotalScore() + 1,
		},
		p.forkConfig)

	return newFlow(p, parent, rt, features, nil, 0, nil), nil
}

func (p *Packer) gasLimit(parentGasLimit uint64) uint64 {
	if p.targetGasLimit != 0 {
		return block.GasLimit(p.targetGasLimit).Qualify(parentGasLimit)
	}
	return parentGasLimit
}

// SetTargetGasLimit set target gas limit, the Packer will adjust block gas limit close to
// it as it can.
func (p *Packer) SetTargetGasLimit(gl uint64) {
	p.targetGasLimit = gl
}
