// Copyright (c) 2026 The VeChainThor developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package service

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vechain/thor/v2/genesis"
	"github.com/vechain/thor/v2/test/datagen"
	"github.com/vechain/thor/v2/test/testchain"
	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/tx"
)

func newTestBackend(t *testing.T) *Backend {
	tc, err := testchain.NewDefault()
	require.NoError(t, err)
	return NewBackend(tc.Repo(), tc.Stater(), tc.Engine())
}

func TestExampleMethods(t *testing.T) {
	b := newTestBackend(t)

	// genesis-only chain: best block number is 0
	num, err := NewEth(b).BlockNumber()
	require.NoError(t, err)
	assert.Equal(t, hexutil.Uint64(0), num)

	// chainId and net_version are deterministic per genesis but value not asserted; must not error
	chainID, err := NewEth(b).ChainId()
	require.NoError(t, err)
	require.NotNil(t, chainID)

	version := NewNet(b).Version()
	require.NotEmpty(t, version)
}

func TestGetBalance(t *testing.T) {
	b := newTestBackend(t)
	eth := NewEth(b)

	addr := genesis.DevAccounts()[0].Address
	want, ok := new(big.Int).SetString(genesis.InitialDevAccountBalance, 10)
	require.True(t, ok)

	for _, tag := range []*string{nil, new("latest"), new("earliest"), new("finalized")} {
		bal, err := eth.GetBalance(addr, tag)
		require.NoError(t, err)
		assert.Zero(t, want.Cmp((*big.Int)(bal)))
	}

	// account absent from genesis has zero balance
	bal, err := eth.GetBalance(thor.Address{}, nil)
	require.NoError(t, err)
	assert.Zero(t, (*big.Int)(bal).Sign())

	// unknown block -> error, not zero balance
	_, err = eth.GetBalance(addr, new("0x5"))
	assert.Error(t, err)
}

func TestGetBalanceAtHistory(t *testing.T) {
	tc, err := testchain.NewDefault()
	require.NoError(t, err)
	b := NewBackend(tc.Repo(), tc.Stater(), tc.Engine())
	eth := NewEth(b)

	sender := genesis.DevAccounts()[0]
	recipient := datagen.RandAddress()
	transferred := big.NewInt(1000)

	clause := tx.NewClause(&recipient).WithValue(transferred)
	require.NoError(t, tc.MintClauses(sender, []*tx.Clause{clause}))

	// state must be opened at the requested block's root, not always best
	bal, err := eth.GetBalance(recipient, new("earliest"))
	require.NoError(t, err)
	assert.Zero(t, (*big.Int)(bal).Sign())

	bal, err = eth.GetBalance(recipient, new("latest"))
	require.NoError(t, err)
	assert.Zero(t, transferred.Cmp((*big.Int)(bal)))
}
