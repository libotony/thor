// Copyright (c) 2025 The VeChainThor developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package comm

import (
	"bytes"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/rlp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vechain/thor/v2/comm/proto"
	"github.com/vechain/thor/v2/forkid"
	"github.com/vechain/thor/v2/genesis"
	"github.com/vechain/thor/v2/p2p"
	"github.com/vechain/thor/v2/p2p/discover"
	"github.com/vechain/thor/v2/test/testchain"
	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/tx"
	"github.com/vechain/thor/v2/txpool"
)

func TestHandleRPC_MsgNewTx(t *testing.T) {
	chain, err := testchain.NewWithFork(&thor.SoloFork, 180)
	require.NoError(t, err)
	repo := chain.Repo()

	pool := txpool.New(repo, chain.Stater(), txpool.Options{
		Limit:           10000,
		LimitPerAccount: 16,
		MaxLifetime:     10 * time.Minute,
	}, &thor.SoloFork)
	defer pool.Close()

	comm := New(repo, pool, nil)
	peer := newPeer(p2p.NewPeer(discover.NodeID{}, "test", nil), stubMsgReadWriter{}, proto.Version)

	to, _ := thor.ParseAddress("0x7567d83b7b8d80addcb281a71d54fc7b3364ffed")
	chainTag := repo.ChainTag()
	testTx := tx.NewBuilder(tx.TypeLegacy).
		ChainTag(chainTag).
		BlockRef(tx.NewBlockRef(0)).
		Expiration(100).
		GasPriceCoef(0).
		Gas(21000).
		Nonce(1).
		Clause(tx.NewClause(&to).WithValue(big.NewInt(1))).
		Build()
	testTx = tx.MustSign(testTx, genesis.DevAccounts()[0].PrivateKey)
	txHash := testTx.Hash()
	txID := testTx.ID()

	txData, err := rlp.EncodeToBytes(testTx)
	require.NoError(t, err)

	t.Run("valid transaction", func(t *testing.T) {
		writeCalled := false
		var writtenData any

		write := func(data any) {
			writeCalled = true
			writtenData = data
		}

		msg := &p2p.Msg{
			Code:    proto.MsgNewTx,
			Size:    uint32(len(txData)),
			Payload: bytes.NewReader(txData),
		}

		txsToSync := &txsToSync{}

		err := comm.handleRPC(peer, msg, write, txsToSync)
		assert.NoError(t, err)

		assert.True(t, peer.IsTransactionKnown(txHash), "transaction should be marked on peer")

		addedTx := pool.Get(txID)
		assert.NotNil(t, addedTx, "transaction should be added to pool")
		if addedTx != nil {
			assert.Equal(t, testTx.Hash(), addedTx.Hash(), "added transaction should match")
		}

		assert.True(t, writeCalled, "write function should be called")
		assert.Equal(t, &struct{}{}, writtenData, "write should be called with empty struct")
	})

	t.Run("transaction exceeds size limit", func(t *testing.T) {
		writeCalled := false

		write := func(data any) {
			writeCalled = true
		}

		msg := &p2p.Msg{
			Code:    proto.MsgNewTx,
			Size:    maxTxSize + 1,
			Payload: bytes.NewReader(txData),
		}

		txsToSync := &txsToSync{}

		err := comm.handleRPC(peer, msg, write, txsToSync)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "payload size: exceeds limit")

		assert.False(t, writeCalled, "write should not be called on error")
	})

	t.Run("decode error", func(t *testing.T) {
		writeCalled := false

		write := func(data any) {
			writeCalled = true
		}

		invalidData := []byte{0x01, 0x02, 0x03}
		msg := &p2p.Msg{
			Code:    proto.MsgNewTx,
			Size:    uint32(len(invalidData)),
			Payload: bytes.NewReader(invalidData),
		}

		txsToSync := &txsToSync{}

		err := comm.handleRPC(peer, msg, write, txsToSync)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "decode msg")

		assert.False(t, writeCalled, "write should not be called on error")
	})

	t.Run("too many clauses", func(t *testing.T) {
		writeCalled := false

		write := func(data any) {
			writeCalled = true
		}

		b := tx.NewBuilder(tx.TypeLegacy).
			ChainTag(chainTag).
			BlockRef(tx.NewBlockRef(0)).
			Expiration(100).
			GasPriceCoef(0).
			Gas(21000).
			Nonce(2)
		for i := 0; i <= tx.MaxClausesPerTx; i++ {
			b = b.Clause(tx.NewClause(nil))
		}
		manyClauseTx := b.Build()

		manyClauseTxData, err := rlp.EncodeToBytes(manyClauseTx)
		require.NoError(t, err)

		msg := &p2p.Msg{
			Code:    proto.MsgNewTx,
			Size:    uint32(len(manyClauseTxData)),
			Payload: bytes.NewReader(manyClauseTxData),
		}

		txsToSync := &txsToSync{}

		err = comm.handleRPC(peer, msg, write, txsToSync)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "clause count exceeds limit")

		assert.False(t, writeCalled, "write should not be called on error")
	})

	t.Run("transaction at size limit boundary", func(t *testing.T) {
		writeCalled := false
		var writtenData any

		write := func(data any) {
			writeCalled = true
			writtenData = data
		}

		msg := &p2p.Msg{
			Code:    proto.MsgNewTx,
			Size:    maxTxSize,
			Payload: bytes.NewReader(txData),
		}

		txsToSync := &txsToSync{}

		err := comm.handleRPC(peer, msg, write, txsToSync)
		assert.NoError(t, err)

		assert.True(t, writeCalled, "write function should be called")
		assert.Equal(t, &struct{}{}, writtenData, "write should be called with empty struct")
	})
}

func TestHandleRPC_MsgGetStatus(t *testing.T) {
	chain, pool := newHandshakeChain(t)
	repo := chain.Repo()
	comm := New(repo, pool, &thor.SoloFork)

	getStatus := func(version uint) any {
		t.Helper()
		peer := newPeer(p2p.NewPeer(discover.NodeID{}, "test", nil), stubMsgReadWriter{}, version)
		payload, err := rlp.EncodeToBytes(&struct{}{})
		require.NoError(t, err)
		msg := &p2p.Msg{Code: proto.MsgGetStatus, Size: uint32(len(payload)), Payload: bytes.NewReader(payload)}

		var written any
		require.NoError(t, comm.handleRPC(peer, msg, func(data any) { written = data }, &txsToSync{}))
		require.NotNil(t, written)
		return written
	}
	best := repo.BestBlockSummary().Header
	genesisID := repo.GenesisBlock().Header().ID()

	t.Run("thor/2 peer gets 5 field status with live fork id", func(t *testing.T) {
		written := getStatus(proto.Version)
		require.IsType(t, &proto.Status{}, written)

		// strict decode: a missing ForkID field would fail here
		enc, err := rlp.EncodeToBytes(written)
		require.NoError(t, err)
		var got proto.Status
		require.NoError(t, rlp.DecodeBytes(enc, &got))

		assert.Equal(t, genesisID, got.GenesisBlockID)
		assert.Equal(t, best.ID(), got.BestBlockID)
		assert.Equal(t, best.TotalScore(), got.TotalScore)
		assert.Equal(t, forkid.NewID(genesisID, &thor.SoloFork, best.Number()), got.ForkID)
		assert.Zero(t, got.ForkID.Next, "head is past the SoloFork INTERSTELLAR height")
	})

	t.Run("thor/1 peer gets strict 4 field status", func(t *testing.T) {
		written := getStatus(proto.V1)
		require.IsType(t, &proto.StatusV1{}, written)

		enc, err := rlp.EncodeToBytes(written)
		require.NoError(t, err)
		// strict decode: a 5th element would be rejected as "too many elements"
		var got proto.StatusV1
		require.NoError(t, rlp.DecodeBytes(enc, &got))

		assert.Equal(t, genesisID, got.GenesisBlockID)
		assert.Equal(t, best.ID(), got.BestBlockID)
		assert.Equal(t, best.TotalScore(), got.TotalScore)
	})
}
