// Copyright (c) 2026 The VeChainThor developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package forkid

import (
	"errors"
	"math/big"
	"slices"
	"testing"

	gethforkid "github.com/ethereum/go-ethereum/core/forkid"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	"github.com/stretchr/testify/assert"

	"github.com/vechain/thor/v2/thor"
)

// gethFixture maps the 8 deduplicated VeChain mainnet heights onto the ascending block
// fields of geth's ChainConfig, seeded with the same genesis hash; shared by both differential tests.
func gethFixture() (*params.ChainConfig, *types.Block, thor.Bytes32, []uint64) {
	heights := []uint64{3_337_300, 4_817_300, 9_254_300, 10_653_500, 13_815_000, 22_084_200, 23_414_400, 25_902_540}
	cfg := &params.ChainConfig{
		ChainID:             big.NewInt(1),
		HomesteadBlock:      new(big.Int).SetUint64(heights[0]),
		EIP150Block:         new(big.Int).SetUint64(heights[1]),
		EIP155Block:         new(big.Int).SetUint64(heights[2]),
		EIP158Block:         new(big.Int).SetUint64(heights[3]),
		ByzantiumBlock:      new(big.Int).SetUint64(heights[4]),
		ConstantinopleBlock: new(big.Int).SetUint64(heights[5]),
		PetersburgBlock:     new(big.Int).SetUint64(heights[6]),
		IstanbulBlock:       new(big.Int).SetUint64(heights[7]),
	}
	genesis := types.NewBlockWithHeader(&types.Header{Number: big.NewInt(0)})
	return cfg, genesis, thor.Bytes32(genesis.Hash()), heights
}

// TestDifferentialGeth checks that both implementations produce the same CRC chain, window by window.
func TestDifferentialGeth(t *testing.T) {
	cfg, genesis, genesisID, heights := gethFixture()
	fc := thor.GetForkConfig(mainnetID)
	for _, h := range heights {
		for _, head := range []uint64{h - 1, h} {
			want := gethforkid.NewID(cfg, genesis, head, 0)
			got := NewID(genesisID, fc, uint32(head))
			assert.Equal(t, want.Hash, got.Hash, "head=%d", head)
			assert.Equal(t, want.Next, got.Next, "head=%d", head)
		}
	}
}

type fakeChain struct {
	cfg     *params.ChainConfig
	genesis *types.Block
	head    uint64
}

func (c *fakeChain) Config() *params.ChainConfig { return c.cfg }
func (c *fakeChain) Genesis() *types.Block       { return c.genesis }
func (c *fakeChain) CurrentHeader() *types.Header {
	return &types.Header{Number: new(big.Int).SetUint64(c.head)}
}

// TestDifferentialGethFilter pairs each window start and each fork height-1 as local head with each window checksum
// as remote ID under 3 kinds of Next; both implementations must agree on the verdict class
// (accepted / remote stale / local incompatible).
func TestDifferentialGethFilter(t *testing.T) {
	cfg, genesis, genesisID, heights := gethFixture()
	fc := thor.GetForkConfig(mainnetID)
	starts := append([]uint64{0}, heights...)
	heads := slices.Clone(starts)
	for _, h := range heights {
		heads = append(heads, h-1)
	}

	classify := func(err, stale, incompatible error) string {
		switch {
		case err == nil:
			return "ok"
		case errors.Is(err, stale):
			return "remote stale"
		case errors.Is(err, incompatible):
			return "local incompatible"
		}
		return "unexpected: " + err.Error()
	}
	seen := map[string]bool{}
	for _, head := range heads {
		gethFilter := gethforkid.NewFilter(&fakeChain{cfg, genesis, head})
		filter := NewFilter(genesisID, fc, func() uint32 { return uint32(head) })
		for j, start := range starts {
			hash := gethforkid.NewID(cfg, genesis, start, 0).Hash
			nexts := []uint64{0, 24_000_000}
			if j < len(heights) {
				nexts = append(nexts, heights[j])
			}
			for _, next := range nexts {
				want := classify(gethFilter(gethforkid.ID{Hash: hash, Next: next}), gethforkid.ErrRemoteStale, gethforkid.ErrLocalIncompatibleOrStale)
				got := classify(filter(ID{Hash: hash, Next: next}), ErrRemoteStale, ErrLocalIncompatibleOrStale)
				assert.Equal(t, want, got, "head=%d remote window=%d next=%d", head, j, next)
				seen[want] = true
			}
		}
	}
	assert.Len(t, seen, 3, "matrix must cover all three outcomes")
}
