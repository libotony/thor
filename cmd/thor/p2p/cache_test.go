// Copyright (c) 2026 The VeChainThor developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package p2p

import (
	"bytes"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fakeCacheNode(seed byte) *enode.Node {
	// deterministic key yields a valid v4 node
	key, _ := crypto.ToECDSA(bytes.Repeat([]byte{seed}, 32))
	return enode.NewV4(&key.PublicKey, net.IPv4(10, 0, 0, seed), 30303, 30303)
}

func TestNodesCacheRoundTripAndTolerance(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "peers-v2.cache")
	nodes := []*enode.Node{fakeCacheNode(1), fakeCacheNode(2)}
	require.NoError(t, saveNodesCache(path, nodes))
	got := loadNodesCache(path)
	require.Len(t, got, 2)
	assert.Equal(t, nodes[0].ID(), got[0].ID())
	assert.Equal(t, nodes[0].TCP(), got[0].TCP())

	// TCP != UDP survives through the discport query parameter
	key, _ := crypto.ToECDSA(bytes.Repeat([]byte{3}, 32))
	split := enode.NewV4(&key.PublicKey, net.IPv4(10, 0, 0, 3), 30303, 30301)
	require.NoError(t, saveNodesCache(path, []*enode.Node{split}))
	got = loadNodesCache(path)
	require.Len(t, got, 1)
	assert.Equal(t, 30303, got[0].TCP())
	assert.Equal(t, 30301, got[0].UDP())
	assert.True(t, split.IP().Equal(got[0].IP()))

	// missing file, corrupted content and legacy RLP all yield nil without panic
	assert.Nil(t, loadNodesCache(filepath.Join(dir, "absent")))
	require.NoError(t, os.WriteFile(path, []byte("{broken"), 0600))
	assert.Nil(t, loadNodesCache(path))
	require.NoError(t, os.WriteFile(path, []byte{0xc0, 0x01, 0x02}, 0600)) // fake RLP
	assert.Nil(t, loadNodesCache(path))
}
