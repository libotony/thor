// Copyright (c) 2026 The VeChainThor developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package runtime_test

import (
	"math"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	gomath "github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/assert"

	"github.com/vechain/thor/v2/block"
	"github.com/vechain/thor/v2/builtin"
	"github.com/vechain/thor/v2/chain"
	"github.com/vechain/thor/v2/genesis"
	"github.com/vechain/thor/v2/muxdb"
	"github.com/vechain/thor/v2/runtime"
	"github.com/vechain/thor/v2/state"
	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/trie"
	"github.com/vechain/thor/v2/tx"
	"github.com/vechain/thor/v2/xenv"
)

// setupEthTxRuntime builds a devnet with GALACTICA active at block 1 and
// returns (repo at b1, fresh state at b0, baseFee at b1, b1 timestamp).
// The state isn't advanced past b0 because b1 has no txs; ctx.BaseFee /
// ctx.Number / ctx.Time supplied at runtime time are sufficient to land
// in the post-galactica branch of runtime.go.
func setupEthTxRuntime(t *testing.T) (*chain.Repository, *state.State, *big.Int, uint64) {
	t.Helper()
	db := muxdb.NewMem()

	fc := &thor.SoloFork
	hayabusaTP := uint32(math.MaxUint32)
	thor.SetConfig(thor.Config{HayabusaTP: &hayabusaTP})
	fc.HAYABUSA = math.MaxUint32
	fc.GALACTICA = 1

	g := genesis.NewDevnetWithConfig(genesis.DevConfig{ForkConfig: fc})
	b0, _, _, err := g.Build(state.NewStater(db))
	assert.Nil(t, err)
	repo, _ := chain.NewRepository(db, b0)

	st := state.New(db, trie.Root{Hash: b0.Header().StateRoot()})
	ver := trie.Version{Major: b0.Header().Number() + 1, Minor: 0}
	stg, err := st.Stage(ver)
	assert.Nil(t, err)
	root, err := stg.Commit()
	assert.Nil(t, err)

	baseFee := big.NewInt(thor.InitialBaseFee)
	b1 := new(block.Builder).
		ParentID(b0.Header().ID()).
		Timestamp(b0.Header().Timestamp() + thor.BlockInterval()).
		GasLimit(b0.Header().GasLimit()).
		BaseFee(baseFee).
		StateRoot(root).
		Build()
	repo.AddBlock(b1, nil, 0, true)

	st = state.New(db, trie.Root{Hash: b0.Header().StateRoot()})
	return repo, st, baseFee, b1.Header().Timestamp()
}

func TestEthDynFee_PlainTransfer(t *testing.T) {
	repo, st, baseFee, blockTime := setupEthTxRuntime(t)

	origin := genesis.DevAccounts()[0]
	recipient := genesis.DevAccounts()[1]
	beneficiary := thor.BytesToAddress([]byte("proposer"))

	value := big.NewInt(1000)
	maxFee := new(big.Int).Mul(baseFee, big.NewInt(2))
	maxPriority := new(big.Int).Set(baseFee)
	gas := uint64(21000)

	addr := recipient.Address
	trx := tx.NewBuilder(tx.TypeEthDynamicFee).
		Gas(gas).
		MaxFeePerGas(maxFee).
		MaxPriorityFeePerGas(maxPriority).
		ChainID(0).
		Nonce(1).
		Clause(tx.NewClause(&addr).WithValue(value)).
		Build()
	trx = tx.MustSign(trx, origin.PrivateKey)

	prevOriginEnergy, err := builtin.Energy.Native(st, blockTime).Get(origin.Address)
	assert.Nil(t, err)
	prevBeneficiaryEnergy, err := builtin.Energy.Native(st, blockTime).Get(beneficiary)
	assert.Nil(t, err)

	rt := runtime.New(
		repo.NewChain(repo.BestBlockSummary().Header.ID()),
		st,
		&xenv.BlockContext{
			Time:        blockTime,
			Number:      repo.BestBlockSummary().Header.Number() + 1,
			GasLimit:    repo.BestBlockSummary().Header.GasLimit(),
			BaseFee:     baseFee,
			Beneficiary: beneficiary,
		},
		&thor.SoloFork,
	)

	receipt, err := rt.ExecuteTransaction(trx)
	assert.Nil(t, err)
	assert.False(t, receipt.Reverted, "plain transfer must not revert")

	// Intrinsic gas: pure transfer with empty data → 21000.
	assert.Equal(t, uint64(21000), receipt.GasUsed)

	// Tip routing: priorityFee × gasUsed → beneficiary.
	currBeneficiaryEnergy, err := builtin.Energy.Native(st, blockTime).Get(beneficiary)
	assert.Nil(t, err)
	tipDelta := new(big.Int).Sub(currBeneficiaryEnergy, prevBeneficiaryEnergy)
	expectedTip := new(big.Int).Mul(maxPriority, big.NewInt(int64(receipt.GasUsed)))
	assert.Equal(t, expectedTip, tipDelta, "tip = maxPriority × gasUsed")

	// Origin paid: gasUsed × effectiveGasPrice.
	currOriginEnergy, err := builtin.Energy.Native(st, blockTime).Get(origin.Address)
	assert.Nil(t, err)
	originDelta := new(big.Int).Sub(prevOriginEnergy, currOriginEnergy)
	expectedPaid := new(big.Int).Mul(maxFee, big.NewInt(int64(receipt.GasUsed)))
	assert.Equal(t, expectedPaid, originDelta, "origin paid = effectiveGasPrice × gasUsed")
	assert.Equal(t, expectedPaid, receipt.Paid)
	assert.Equal(t, origin.Address, receipt.GasPayer)
}

func TestEthDynFee_ContractCreation(t *testing.T) {
	repo, st, baseFee, blockTime := setupEthTxRuntime(t)

	origin := genesis.DevAccounts()[0]
	beneficiary := thor.BytesToAddress([]byte("proposer"))

	// Minimal valid creation bytecode: STOP. Empty deployed code, dataGas = 4 (one zero byte).
	code := []byte{0x00}

	maxFee := new(big.Int).Mul(baseFee, big.NewInt(2))
	maxPriority := new(big.Int).Set(baseFee)

	trx := tx.NewBuilder(tx.TypeEthDynamicFee).
		Gas(100000).
		MaxFeePerGas(maxFee).
		MaxPriorityFeePerGas(maxPriority).
		ChainID(0).
		Nonce(2).
		Clause(tx.NewClause(nil).WithData(code)). // To == nil → contract creation
		Build()
	trx = tx.MustSign(trx, origin.PrivateKey)

	rt := runtime.New(
		repo.NewChain(repo.BestBlockSummary().Header.ID()),
		st,
		&xenv.BlockContext{
			Time:        blockTime,
			Number:      repo.BestBlockSummary().Header.Number() + 1,
			GasLimit:    repo.BestBlockSummary().Header.GasLimit(),
			BaseFee:     baseFee,
			Beneficiary: beneficiary,
		},
		&thor.SoloFork,
	)

	receipt, err := rt.ExecuteTransaction(trx)
	assert.Nil(t, err)
	assert.False(t, receipt.Reverted, "creation must not revert")

	// Intrinsic gas floor: TxGas + ClauseGasContractCreation + dataGas(0x00)
	// = 5000 + 48000 + 4 = 53004. gasUsed >= floor.
	assert.GreaterOrEqual(t, receipt.GasUsed, uint64(53004))

	// Eth tx uses Ethereum's nonce-based rule: CreateAddress(origin, nonce-before-increment).
	// On-state nonce starts at 0 for the genesis account.
	assert.Len(t, receipt.Outputs, 1)
	expectedAddr := thor.Address(crypto.CreateAddress(common.Address(origin.Address), 0))
	exists, existsErr := st.Exists(expectedAddr)
	assert.Nil(t, existsErr)
	assert.True(t, exists, "contract account must exist at eth-derived address")
}

// TestEthDynFee_RevertPreservesNonce guards eth tx revert semantics: when a
// clause hits VMErr, the receipt is marked reverted but the sender nonce
// increment must persist (matches Ethereum: failed txs still consume nonce).
func TestEthDynFee_RevertPreservesNonce(t *testing.T) {
	repo, st, baseFee, blockTime := setupEthTxRuntime(t)

	origin := genesis.DevAccounts()[0]
	beneficiary := thor.BytesToAddress([]byte("proposer"))

	// Init code = INVALID opcode → ErrInvalidOpCode → VMErr.
	// CREATE path: evm.create increments nonce before its snapshot, so the
	// increment is preserved across EVM-internal RevertToSnapshot.
	maxFee := new(big.Int).Mul(baseFee, big.NewInt(2))
	maxPriority := new(big.Int).Set(baseFee)

	trx := tx.NewBuilder(tx.TypeEthDynamicFee).
		Gas(100000).
		MaxFeePerGas(maxFee).
		MaxPriorityFeePerGas(maxPriority).
		ChainID(0).
		Nonce(0).
		Clause(tx.NewClause(nil).WithData([]byte{0xfe})).
		Build()
	trx = tx.MustSign(trx, origin.PrivateKey)

	prevNonce, err := st.GetNonce(origin.Address)
	assert.Nil(t, err)
	assert.Equal(t, uint64(0), prevNonce)

	rt := runtime.New(
		repo.NewChain(repo.BestBlockSummary().Header.ID()),
		st,
		&xenv.BlockContext{
			Time:        blockTime,
			Number:      repo.BestBlockSummary().Header.Number() + 1,
			GasLimit:    repo.BestBlockSummary().Header.GasLimit(),
			BaseFee:     baseFee,
			Beneficiary: beneficiary,
		},
		&thor.SoloFork,
	)

	receipt, err := rt.ExecuteTransaction(trx)
	assert.Nil(t, err, "ExecuteTransaction should not error on clause-level revert")
	assert.True(t, receipt.Reverted, "eth tx must be marked reverted on VMErr")
	assert.Nil(t, receipt.Outputs, "outputs must be cleared on revert")

	// Nonce persists post-revert (Ethereum semantics).
	currNonce, err := st.GetNonce(origin.Address)
	assert.Nil(t, err)
	assert.Equal(t, uint64(1), currNonce, "sender nonce must increment even on reverted eth tx")

	// Contract account must NOT exist at the eth-derived address.
	contractAddr := thor.Address(crypto.CreateAddress(common.Address(origin.Address), 0))
	exists, err := st.Exists(contractAddr)
	assert.Nil(t, err)
	assert.False(t, exists, "failed init must not leave a contract account")
}

func TestEthDynFee_SponsoredCall(t *testing.T) {
	repo, st, baseFee, blockTime := setupEthTxRuntime(t)

	origin := genesis.DevAccounts()[0]
	sponsor := genesis.DevAccounts()[2]
	beneficiary := thor.BytesToAddress([]byte("proposer"))

	// Set up Prototype contract as a sponsored target where origin is a user
	// and sponsor is the selected sponsor.
	target := builtin.Prototype.Address
	bind := builtin.Prototype.Native(st).Bind(target)
	err := bind.SetCreditPlan(gomath.MaxBig256, big.NewInt(1000))
	assert.Nil(t, err)
	err = bind.AddUser(origin.Address, blockTime)
	assert.Nil(t, err)
	err = bind.Sponsor(sponsor.Address, true)
	assert.Nil(t, err)
	bind.SelectSponsor(sponsor.Address)

	// Fund sponsor with enough energy to cover gas.
	builtin.Energy.Native(st, blockTime).Add(sponsor.Address, gomath.MaxBig256)

	prevSponsorEnergy, err := builtin.Energy.Native(st, blockTime).Get(sponsor.Address)
	assert.Nil(t, err)
	prevOriginEnergy, err := builtin.Energy.Native(st, blockTime).Get(origin.Address)
	assert.Nil(t, err)

	maxFee := new(big.Int).Mul(baseFee, big.NewInt(2))
	maxPriority := new(big.Int).Set(baseFee)

	// Encode a no-op-ish call: Prototype.master(target). Read-only, gas-cheap.
	method, found := builtin.Prototype.ABI.MethodByName("master")
	assert.True(t, found)
	callData, err := method.EncodeInput(target)
	assert.Nil(t, err)

	trx := tx.NewBuilder(tx.TypeEthDynamicFee).
		Gas(100000).
		MaxFeePerGas(maxFee).
		MaxPriorityFeePerGas(maxPriority).
		ChainID(0).
		Nonce(3).
		Clause(tx.NewClause(&target).WithData(callData)).
		Build()
	trx = tx.MustSign(trx, origin.PrivateKey)

	rt := runtime.New(
		repo.NewChain(repo.BestBlockSummary().Header.ID()),
		st,
		&xenv.BlockContext{
			Time:        blockTime,
			Number:      repo.BestBlockSummary().Header.Number() + 1,
			GasLimit:    repo.BestBlockSummary().Header.GasLimit(),
			BaseFee:     baseFee,
			Beneficiary: beneficiary,
		},
		&thor.SoloFork,
	)

	receipt, err := rt.ExecuteTransaction(trx)
	assert.Nil(t, err)
	assert.False(t, receipt.Reverted)

	// Sponsor pays.
	assert.Equal(t, sponsor.Address, receipt.GasPayer)
	currSponsorEnergy, err := builtin.Energy.Native(st, blockTime).Get(sponsor.Address)
	assert.Nil(t, err)
	assert.True(t, currSponsorEnergy.Cmp(prevSponsorEnergy) < 0, "sponsor energy decreased")

	// Origin DOES NOT pay.
	currOriginEnergy, err := builtin.Energy.Native(st, blockTime).Get(origin.Address)
	assert.Nil(t, err)
	assert.Equal(t, prevOriginEnergy, currOriginEnergy, "origin energy unchanged")
}

func TestEthDynFee_BaseFeeFloor(t *testing.T) {
	_, st, baseFee, _ := setupEthTxRuntime(t)

	origin := genesis.DevAccounts()[0]
	addr := genesis.DevAccounts()[1].Address

	// maxFee BELOW baseFee.
	maxFee := new(big.Int).Sub(baseFee, big.NewInt(1))

	trx := tx.NewBuilder(tx.TypeEthDynamicFee).
		Gas(21000).
		MaxFeePerGas(maxFee).
		MaxPriorityFeePerGas(big.NewInt(0)).
		ChainID(0).
		Nonce(4).
		Clause(tx.NewClause(&addr).WithValue(big.NewInt(1))).
		Build()
	trx = tx.MustSign(trx, origin.PrivateKey)

	resolved, err := runtime.ResolveTransaction(trx)
	assert.Nil(t, err, "resolution itself should pass — baseFee is checked at BuyGas, not resolution")

	_, _, _, _, _, err = resolved.BuyGas(st, 0, baseFee)
	assert.ErrorContains(t, err, "gas price is less than block base fee")
}

func TestEthDynFee_InsufficientBalance(t *testing.T) {
	_, st, baseFee, blockTime := setupEthTxRuntime(t)

	// Fresh key with zero energy — devnet DevAccounts have huge balances, so we need a new account.
	pk, err := crypto.GenerateKey()
	assert.Nil(t, err)
	pauper := thor.Address(crypto.PubkeyToAddress(pk.PublicKey))

	// Sanity: pauper has zero energy.
	pauperEnergy, err := builtin.Energy.Native(st, blockTime).Get(pauper)
	assert.Nil(t, err)
	assert.Equal(t, 0, pauperEnergy.Sign())

	addr := genesis.DevAccounts()[1].Address
	trx := tx.NewBuilder(tx.TypeEthDynamicFee).
		Gas(21000).
		MaxFeePerGas(new(big.Int).Mul(baseFee, big.NewInt(2))).
		MaxPriorityFeePerGas(new(big.Int).Set(baseFee)).
		ChainID(0).
		Nonce(5).
		Clause(tx.NewClause(&addr).WithValue(big.NewInt(1))).
		Build()
	trx = tx.MustSign(trx, pk)

	resolved, err := runtime.ResolveTransaction(trx)
	assert.Nil(t, err)

	_, _, _, _, _, err = resolved.BuyGas(st, blockTime, baseFee)
	assert.ErrorContains(t, err, "insufficient energy")
}
