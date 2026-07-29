// Copyright (c) 2026 The VeChainThor developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package service

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vechain/thor/v2/test/testchain"
)

func TestSummaryAt(t *testing.T) {
	tc, err := testchain.NewDefault()
	require.NoError(t, err)
	b := NewBackend(tc.Repo(), tc.Stater(), tc.Engine())

	genesisID := tc.GenesisBlock().Header().ID()

	// on a genesis-only chain every valid form resolves to the genesis block
	valid := []struct {
		name string
		tag  *string
	}{
		{"nil defaults to latest", nil},
		{"latest", new("latest")},
		{"pending", new("pending")},
		{"earliest", new("earliest")},
		{"safe", new("safe")},
		{"finalized", new("finalized")},
		{"hex number", new("0x0")},
		{"block hash", new(genesisID.String())},
	}
	for _, tt := range valid {
		t.Run(tt.name, func(t *testing.T) {
			sum, err := b.summaryAt(tt.tag)
			require.NoError(t, err)
			assert.Equal(t, genesisID, sum.Header.ID())
		})
	}

	invalid := []struct {
		name string
		tag  string
	}{
		{"thor tag best", "best"},
		{"thor tag justified", "justified"},
		{"thor tag next", "next"},
		{"decimal number", "0"},
		{"garbage", "zzz"},
		{"empty string", ""},
		{"beyond uint32", "0x100000000"},
	}
	for _, tt := range invalid {
		t.Run(tt.name, func(t *testing.T) {
			_, err := b.summaryAt(&tt.tag)
			require.Error(t, err)
			var de invalidParamsError
			require.ErrorAs(t, err, &de)
			assert.Equal(t, -32602, de.ErrorCode())
		})
	}

	// unknown block number: revision parses fine, lookup fails -> geth-style "header not found"
	t.Run("unknown block number", func(t *testing.T) {
		tag := "0x5"
		_, err := b.summaryAt(&tag)
		require.Error(t, err)
		assert.Equal(t, "header not found", err.Error())
	})

	// all-zero hash: GetSummary's "invalid revision" case, also treated as absent block
	t.Run("zero hash", func(t *testing.T) {
		tag := "0x" + strings.Repeat("0", 64)
		_, err := b.summaryAt(&tag)
		require.Error(t, err)
		assert.Equal(t, "header not found", err.Error())
	})
}

func TestSummaryAtMultiBlock(t *testing.T) {
	tc, err := testchain.NewDefault()
	require.NoError(t, err)
	b := NewBackend(tc.Repo(), tc.Stater(), tc.Engine())

	require.NoError(t, tc.MintBlock())
	require.NoError(t, tc.MintBlock())

	sum, err := b.summaryAt(new("earliest"))
	require.NoError(t, err)
	assert.Equal(t, uint32(0), sum.Header.Number())

	sum, err = b.summaryAt(nil)
	require.NoError(t, err)
	assert.Equal(t, uint32(2), sum.Header.Number())

	sum, err = b.summaryAt(new("latest"))
	require.NoError(t, err)
	assert.Equal(t, uint32(2), sum.Header.Number())

	sum, err = b.summaryAt(new("0x1"))
	require.NoError(t, err)
	assert.Equal(t, uint32(1), sum.Header.Number())
}
