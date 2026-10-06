// Copyright (c) 2018 The VeChainThor developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package proto

import (
	"encoding/hex"
	"runtime"
	"testing"

	"github.com/ethereum/go-ethereum/rlp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vechain/thor/v2/forkid"
	"github.com/vechain/thor/v2/thor"
)

func TestBlockByIDResult_DecodeRLP(t *testing.T) {
	t.Run("rejects more than MaxBlockByIDResult", func(t *testing.T) {
		data, err := rlp.EncodeToBytes([]rlp.RawValue{
			[]byte{0x01},
			[]byte{0x02},
		})
		require.NoError(t, err)

		var result blockByIDResult
		err = rlp.DecodeBytes(data, &result)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "exceeds limit")
	})

	t.Run("accepts empty result", func(t *testing.T) {
		data, err := rlp.EncodeToBytes([]rlp.RawValue{})
		require.NoError(t, err)

		var result blockByIDResult
		require.NoError(t, rlp.DecodeBytes(data, &result))
		assert.Empty(t, result)
	})

	t.Run("accepts single block", func(t *testing.T) {
		data, err := rlp.EncodeToBytes([]rlp.RawValue{[]byte{0x01}})
		require.NoError(t, err)

		var result blockByIDResult
		require.NoError(t, rlp.DecodeBytes(data, &result))
		assert.Len(t, result, 1)
	})

	// The point of the fix: rejecting a huge list must not cost in proportion
	// to its size.
	t.Run("allocation is decoupled from input size", func(t *testing.T) {
		measure := func(n int) uint64 {
			oversized := make([]rlp.RawValue, n)
			for i := range oversized {
				oversized[i] = []byte{0x80}
			}
			data, err := rlp.EncodeToBytes(oversized)
			require.NoError(t, err)

			var m0, m1 runtime.MemStats
			runtime.GC()
			runtime.ReadMemStats(&m0)

			var result blockByIDResult
			_ = rlp.DecodeBytes(data, &result)

			runtime.ReadMemStats(&m1)
			runtime.KeepAlive(result)
			return m1.TotalAlloc - m0.TotalAlloc
		}

		justOver := measure(2)
		wayOver := measure(2_000_000)
		t.Logf("just-over=%d B  way-over=%d B", justOver, wayOver)
		assert.Less(t, wayOver, uint64(64*1024))
	})
}

func TestBlocksFromNumberResult_DecodeRLP(t *testing.T) {
	t.Run("rejects more than MaxBlocksFromNumber", func(t *testing.T) {
		oversized := make([]rlp.RawValue, MaxBlocksFromNumber+1)
		for i := range oversized {
			// byte(i)&0x7f keeps every element within the single-byte
			// self-encoded RLP range so it's a valid raw value on its own.
			oversized[i] = []byte{byte(i) & 0x7f}
		}
		data, err := rlp.EncodeToBytes(oversized)
		require.NoError(t, err)

		var result blocksFromNumberResult
		err = rlp.DecodeBytes(data, &result)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "exceeds limit")
	})

	t.Run("accepts empty result", func(t *testing.T) {
		data, err := rlp.EncodeToBytes([]rlp.RawValue{})
		require.NoError(t, err)

		var result blocksFromNumberResult
		require.NoError(t, rlp.DecodeBytes(data, &result))
		assert.Empty(t, result)
	})

	t.Run("accepts exactly MaxBlocksFromNumber", func(t *testing.T) {
		blocks := make([]rlp.RawValue, MaxBlocksFromNumber)
		for i := range blocks {
			blocks[i] = []byte{byte(i) & 0x7f}
		}
		data, err := rlp.EncodeToBytes(blocks)
		require.NoError(t, err)

		var result blocksFromNumberResult
		require.NoError(t, rlp.DecodeBytes(data, &result))
		assert.Len(t, result, MaxBlocksFromNumber)
	})
}

func TestStatusRLP(t *testing.T) {
	// ForkID sublist is encoded as [4-byte string, uint], identical to forkid.ID in geth's eth/64 Status
	id := forkid.ID{Hash: [4]byte{0xbb, 0xf3, 0x87, 0x04}, Next: 0}
	data, err := rlp.EncodeToBytes(&id)
	assert.NoError(t, err)
	assert.Equal(t, "c684bbf3870480", hex.EncodeToString(data))

	// v2 Status round trip
	s := Status{
		GenesisBlockID: thor.Bytes32{1}, SysTimestamp: 2,
		BestBlockID: thor.Bytes32{3}, TotalScore: 4, ForkID: id,
	}
	enc, err := rlp.EncodeToBytes(&s)
	assert.NoError(t, err)
	var dec Status
	assert.NoError(t, rlp.DecodeBytes(enc, &dec))
	assert.Equal(t, s, dec)

	// a 4-element payload (StatusV1 bytes) must fail strict decoding as v2
	v1 := StatusV1{GenesisBlockID: thor.Bytes32{1}, SysTimestamp: 2, BestBlockID: thor.Bytes32{3}, TotalScore: 4}
	encV1, err := rlp.EncodeToBytes(&v1)
	assert.NoError(t, err)
	assert.Error(t, rlp.DecodeBytes(encV1, &dec))

	// a 6-element payload (one trailing extra field) must fail strict decoding too
	extra := struct {
		A thor.Bytes32
		B uint64
		C thor.Bytes32
		D uint64
		E forkid.ID
		F uint64
	}{thor.Bytes32{1}, 2, thor.Bytes32{3}, 4, id, 5}
	encX, err := rlp.EncodeToBytes(&extra)
	assert.NoError(t, err)
	assert.Error(t, rlp.DecodeBytes(encX, &dec))

	// v1 peer's view: decoding the new 5-element payload into the old struct also fails (pinned existing behavior)
	var decV1 StatusV1
	assert.Error(t, rlp.DecodeBytes(enc, &decV1))
}
