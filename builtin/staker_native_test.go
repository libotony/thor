// Copyright (c) 2025 The VeChainThor developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package builtin_test

import (
	"errors"
	"math/big"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/vechain/thor/v2/abi"
	"github.com/vechain/thor/v2/block"
	"github.com/vechain/thor/v2/builtin"
	"github.com/vechain/thor/v2/builtin/staker"
	"github.com/vechain/thor/v2/chain"
	"github.com/vechain/thor/v2/genesis"
	"github.com/vechain/thor/v2/muxdb"
	"github.com/vechain/thor/v2/runtime"
	"github.com/vechain/thor/v2/state"
	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/trie"
	"github.com/vechain/thor/v2/tx"
	"github.com/vechain/thor/v2/vm"
	"github.com/vechain/thor/v2/xenv"
)

var (
	errReverted = vm.ErrExecutionReverted
	revertABI   = []byte(`[{"name": "Error","type": "function","inputs": [{"name": "message","type": "string"}]}]`)
)

type ctest struct {
	rt         *runtime.Runtime
	abi        *abi.ABI
	to, caller thor.Address
}

type ccase struct {
	rt         *runtime.Runtime
	abi        *abi.ABI
	to, caller thor.Address
	name       string
	args       []any
	events     tx.Events
	provedWork *big.Int
	txID       thor.Bytes32
	blockRef   tx.BlockRef
	gasPayer   thor.Address
	expiration uint32
	value      *big.Int

	output    *[]any
	vmerr     error
	revertMsg string
}

type TestTxDescription struct {
	t          *testing.T
	abi        *abi.ABI
	methodName string
	address    thor.Address
	acc        genesis.DevAccount
	args       []any
	duplicate  bool
	vet        *big.Int
}

func (c *ctest) Case(name string, args ...any) *ccase {
	return &ccase{
		rt:     c.rt,
		abi:    c.abi,
		to:     c.to,
		caller: c.caller,
		name:   name,
		args:   args,
	}
}

func (c *ccase) To(to thor.Address) *ccase {
	c.to = to
	return c
}

func (c *ccase) Caller(caller thor.Address) *ccase {
	c.caller = caller
	return c
}

func (c *ccase) Value(value *big.Int) *ccase {
	c.value = value
	return c
}

func (c *ccase) ProvedWork(provedWork *big.Int) *ccase {
	c.provedWork = provedWork
	return c
}

func (c *ccase) TxID(txID thor.Bytes32) *ccase {
	c.txID = txID
	return c
}

func (c *ccase) BlockRef(blockRef tx.BlockRef) *ccase {
	c.blockRef = blockRef
	return c
}

func (c *ccase) GasPayer(gasPayer thor.Address) *ccase {
	c.gasPayer = gasPayer
	return c
}

func (c *ccase) Expiration(expiration uint32) *ccase {
	c.expiration = expiration
	return c
}

func (c *ccase) ShouldVMError(err error) *ccase {
	c.vmerr = err
	return c
}

func (c *ccase) ShouldLog(events ...*tx.Event) *ccase {
	c.events = events
	return c
}

func (c *ccase) ShouldOutput(outputs ...any) *ccase {
	c.output = &outputs
	return c
}

func (c *ccase) ShouldRevert(revertMsg string) *ccase {
	c.revertMsg = revertMsg
	c.vmerr = errReverted
	return c
}

func (c *ccase) Assert(t *testing.T) *ccase {
	method, ok := c.abi.MethodByName(c.name)
	assert.True(t, ok, "should have method")

	constant := method.Const()
	stage, err := c.rt.State().Stage(trie.Version{})
	assert.Nil(t, err, "should stage state")
	stateRoot := stage.Hash()

	data, err := method.EncodeInput(c.args...)
	assert.Nil(t, err, "should encode input")

	clause := tx.NewClause(&c.to).WithData(data)
	if c.value != nil {
		clause = clause.WithValue(c.value)
	}

	exec, _ := c.rt.PrepareClause(clause,
		0, 40000000, &xenv.TransactionContext{
			ID:         c.txID,
			Origin:     c.caller,
			GasPrice:   &big.Int{},
			GasPayer:   c.gasPayer,
			ProvedWork: c.provedWork,
			BlockRef:   c.blockRef,
			Expiration: c.expiration,
		})
	vmout, _, err := exec()
	assert.Nil(t, err)
	if constant || vmout.VMErr != nil {
		stage, err := c.rt.State().Stage(trie.Version{})
		assert.Nil(t, err, "should stage state")
		newStateRoot := stage.Hash()
		assert.Equal(t, stateRoot, newStateRoot)
	}
	if c.vmerr != nil {
		assert.Equal(t, c.vmerr, vmout.VMErr)
	} else {
		assert.Nil(t, vmout.VMErr)
	}

	if c.output != nil {
		out, err := method.EncodeOutput((*c.output)...)
		assert.Nil(t, err, "should encode output")
		assert.Equal(t, out, vmout.Data, "should match output")
	}

	if len(c.events) > 0 {
		for _, ev := range c.events {
			found := func() bool {
				for _, outEv := range vmout.Events {
					if reflect.DeepEqual(ev, outEv) {
						return true
					}
				}
				return false
			}()
			assert.True(t, found, "event should appear")
		}
	}

	if c.revertMsg != "" {
		abis, err := abi.New(revertABI)
		assert.NoError(t, err)
		method, ok := abis.MethodByName("Error")
		assert.True(t, ok)
		var revertMsg string
		err = method.DecodeInput(vmout.Data, &revertMsg)
		assert.NoError(t, err)
		assert.Equal(t, c.revertMsg, revertMsg)
	}

	c.output = nil
	c.vmerr = nil
	c.events = nil
	c.revertMsg = ""

	return c
}

func buildGenesis(db *muxdb.MuxDB, proc func(state *state.State) error) *block.Block {
	blk, _, _, err := new(genesis.Builder).
		Timestamp(uint64(time.Now().Unix())).
		State(proc).
		ForkConfig(&thor.NoFork).
		Build(state.NewStater(db))
	if err != nil {
		panic(err)
	}
	return blk
}

func TestStakerContract_Validation(t *testing.T) {
	var (
		master     = thor.BytesToAddress([]byte("master"))
		endorsor   = thor.BytesToAddress([]byte("endorsor"))
		rich       = thor.BytesToAddress([]byte("rich"))
		minStake   = big.NewInt(0).Mul(big.NewInt(250e6), big.NewInt(1e18))
		validator1 = thor.BytesToAddress([]byte("validator1"))
		validator2 = thor.BytesToAddress([]byte("validator2"))
	)

	fc := &thor.SoloFork
	fc.HAYABUSA = 0
	fc.HAYABUSA_TP = 0

	var db = muxdb.NewMem()

	gene := buildGenesis(db, func(state *state.State) error {
		state.SetCode(builtin.Staker.Address, builtin.Staker.RuntimeBytecodes())
		state.SetCode(builtin.Params.Address, builtin.Params.RuntimeBytecodes())
		state.SetCode(builtin.Authority.Address, builtin.Authority.RuntimeBytecodes())

		stakerNative := builtin.Staker.Native(state)
		builtin.Params.Native(state).Set(thor.KeyMaxBlockProposers, big.NewInt(1))

		err := stakerNative.AddValidation(validator1, endorsor, staker.LowStakingPeriod, minStake)
		if err != nil {
			return err
		}
		state.SetBalance(endorsor, big.NewInt(0).Mul(big.NewInt(6000e6), big.NewInt(1e18)))
		state.SetBalance(rich, big.NewInt(0).Mul(big.NewInt(6000e6), big.NewInt(1e18)))

		success, err := stakerNative.Transition(0)
		if err != nil {
			return err
		}
		if !success {
			return errors.New("transition failed")
		}

		return nil
	})

	repo, err := chain.NewRepository(db, gene)
	assert.NoError(t, err)

	bestSummary := repo.BestBlockSummary()
	state := state.NewStater(db).NewState(bestSummary.Root())
	rt := runtime.New(
		repo.NewBestChain(),
		state,
		&xenv.BlockContext{Time: bestSummary.Header.Timestamp()},
		fc,
	)

	test := &ctest{
		rt:     rt,
		abi:    builtin.Staker.ABI,
		to:     builtin.Staker.Address,
		caller: builtin.Staker.Address,
	}

	test.Case("addValidation", master, staker.LowStakingPeriod).
		Value(big.NewInt(0)).
		Caller(endorsor).
		ShouldRevert("staker: stake is empty").
		Assert(t)

	test.Case("addValidation", master, staker.LowStakingPeriod).
		Value(big.NewInt(1)).
		Caller(endorsor).
		ShouldRevert("staker: stake is not multiple of 1VET").
		Assert(t)

	test.Case("addValidation", thor.Address{}, staker.LowStakingPeriod).
		Value(big.NewInt(1e18)).
		Caller(endorsor).
		ShouldRevert("staker: invalid validator").
		Assert(t)

	test.Case("addValidation", master, staker.LowStakingPeriod).
		Value(big.NewInt(1e18)).
		Caller(endorsor).
		ShouldRevert("staker: stake is out of range").
		Assert(t)

	// more than max stake
	test.Case("addValidation", master, staker.LowStakingPeriod).
		Value(big.NewInt(0).Mul(big.NewInt(601e6), big.NewInt(1e18))).
		Caller(endorsor).
		ShouldRevert("staker: stake is out of range").
		Assert(t)

	test.Case("addValidation", validator1, staker.LowStakingPeriod).
		Value(minStake).
		ShouldRevert("staker: validation exists").
		Caller(endorsor).
		Assert(t)

	test.Case("addValidation", master, staker.LowStakingPeriod).
		Value(minStake).
		Caller(endorsor).
		Assert(t)

	test.Case("increaseStake", validator1).
		Value(big.NewInt(0)).
		Caller(endorsor).
		ShouldRevert("staker: stake is empty").
		Assert(t)

	test.Case("increaseStake", validator1).
		Value(big.NewInt(1)).
		Caller(endorsor).
		ShouldRevert("staker: stake is not multiple of 1VET").
		Assert(t)

	test.Case("increaseStake", validator2).
		Value(minStake).
		ShouldRevert("staker: validation not found").
		Caller(endorsor).
		Assert(t)

	test.Case("increaseStake", validator1).
		Value(staker.MaxStake).
		ShouldRevert("staker: total stake reached max limit").
		Caller(endorsor).
		Assert(t)

	test.Case("increaseStake", validator1).
		Value(minStake).
		Caller(endorsor).
		Assert(t)

	// TODO: increase not active or queued
	// TODO: increase signaled exit

}

// 	test.Case("decreaseStake", validation, big.NewInt(0)).
// 		Caller(caller).
// 		ShouldRevert("stake is empty").
// 		Assert(t)

// 	test.Case("decreaseStake", validation, big.NewInt(1)).
// 		Caller(caller).
// 		ShouldRevert("stake is not multiple of 1VET").
// 		Assert(t)

// 	test.Case("addDelegation", validation, uint8(100)).
// 		Caller(delegator).
// 		Value(big.NewInt(0)).
// 		ShouldRevert("stake is empty").
// 		Assert(t)

// 	test.Case("addDelegation", validation, uint8(100)).
// 		Caller(delegator).
// 		Value(big.NewInt(1)).
// 		ShouldRevert("stake is not multiple of 1VET").
// 		Assert(t)
