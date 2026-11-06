// Copyright (c) 2026 The VeChainThor developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package comm

import (
	"net"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/p2p/enr"
	"github.com/stretchr/testify/require"

	"github.com/vechain/thor/v2/forkid"
	"github.com/vechain/thor/v2/test/testchain"
	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/txpool"
)

func signedNode(t *testing.T, entries ...enr.Entry) *enode.Node {
	t.Helper()
	key, _ := crypto.GenerateKey()
	var r enr.Record
	r.Set(enr.IPv4(net.IPv4(127, 0, 0, 1)))
	r.Set(enr.UDP(30303))
	for _, e := range entries {
		r.Set(e)
	}
	require.NoError(t, enode.SignV4(&r, key))
	n, err := enode.New(enode.ValidSchemes, &r)
	require.NoError(t, err)
	return n
}

func TestVechainEntryTailIsForwardCompatible(t *testing.T) {
	id := forkid.ID{Hash: [4]byte{1, 2, 3, 4}, Next: 9}
	n := signedNode(t, enr.WithEntry("vet", struct {
		ForkID forkid.ID
		Extra  string
	}{id, "future"}))
	var got vechainEntry
	require.NoError(t, n.Load(&got))
	require.Equal(t, id, got.ForkID)
	require.Len(t, got.Rest, 1)
}

func TestV5CandidateFilter(t *testing.T) {
	chain, pool := newHandshakeChain(t)
	c := New(chain.Repo(), pool, &thor.SoloFork)
	genesisID := chain.Repo().GenesisBlock().Header().ID()
	head := chain.Repo().BestBlockSummary().Header.Number()
	filter := c.V5CandidateFilter()

	require.False(t, filter(signedNode(t)), "no vet entry")
	require.True(t, filter(signedNode(t, &vechainEntry{ForkID: forkid.NewID(genesisID, &thor.SoloFork, head)})))
	require.False(t, filter(signedNode(t, &vechainEntry{ForkID: forkid.NewID(genesisID, &thor.NoFork, 0)})), "remote stale")
	require.False(t, filter(signedNode(t, &vechainEntry{ForkID: forkid.NewID(thor.Bytes32{0xff}, &thor.SoloFork, head)})), "other genesis")
	require.False(t, filter(signedNode(t, enr.WithEntry("vet", uint(7)))), "malformed vet")
}

func TestENRUpdaterFollowsForkActivation(t *testing.T) {
	fc := thor.SoloFork
	fc.ETH_IST = 5 // benign for empty blocks; pick another non-consensus-critical field if minting across it fails
	chain, err := testchain.NewWithFork(&fc, 180)
	require.NoError(t, err)
	require.NoError(t, chain.MintBlocks(2))
	pool := txpool.New(chain.Repo(), chain.Stater(), txpool.Options{Limit: 10, LimitPerAccount: 2, MaxLifetime: time.Minute}, &fc)
	t.Cleanup(pool.Close)
	c := New(chain.Repo(), pool, &fc)
	t.Cleanup(c.Stop)
	genesisID := chain.Repo().GenesisBlock().Header().ID()

	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	db, err := enode.OpenDB("")
	require.NoError(t, err)
	t.Cleanup(db.Close)
	ln := enode.NewLocalNode(db, key)
	c.StartENRUpdater(ln)

	var e vechainEntry
	require.NoError(t, ln.Node().Load(&e))
	require.Equal(t, forkid.NewID(genesisID, &fc, 2), e.ForkID)
	require.EqualValues(t, 5, e.ForkID.Next)
	seq := ln.Node().Seq()

	// a fresh pointer with the same value must dedup; Node() forces the lazy re-sign
	ln.Set(c.currentENREntry())
	require.Equal(t, seq, ln.Node().Seq(), "same-value Set must not re-sign")

	require.NoError(t, chain.MintBlocks(3)) // head 5 crosses the scheduled fork
	require.Eventually(t, func() bool {
		var e vechainEntry
		return ln.Node().Load(&e) == nil && e.ForkID == forkid.NewID(genesisID, &fc, 5)
	}, 5*time.Second, 20*time.Millisecond)
	var after vechainEntry
	require.NoError(t, ln.Node().Load(&after))
	require.Zero(t, after.ForkID.Next)
	require.Equal(t, seq+1, ln.Node().Seq(), "one re-sign for one fork crossing")
}
