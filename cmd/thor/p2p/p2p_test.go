// Copyright (c) 2024 The VeChainThor developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package p2p

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"testing"

	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/p2p/enr"

	"github.com/vechain/thor/v2/test/datagen"

	"github.com/stretchr/testify/assert"
)

// testNode returns a node identified only by its ID.
func testNode(b byte) *enode.Node {
	return enode.SignNull(new(enr.Record), enode.ID{b})
}

func TestNewThorP2P(t *testing.T) {
	// Generate a private key for testing
	privateKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)

	// Setup test cases
	tests := []struct {
		name                   string
		maxPeers               int
		listenAddr             string
		allowedPeers           []*enode.Node
		cachedPeers            []*enode.Node
		bootstrapNodes         []*enode.Node
		expectedKnownNodes     []*enode.Node
		expectedStaticNodes    []*enode.Node
		expectedDiscoveryNodes []*enode.Node
	}{
		{
			name:                   "Basic instance with no default settings",
			maxPeers:               datagen.RandInt(),
			listenAddr:             datagen.RandHostPort(),
			allowedPeers:           nil,
			cachedPeers:            nil,
			bootstrapNodes:         nil,
			expectedKnownNodes:     nil,
			expectedDiscoveryNodes: fallbackDiscoveryNodes,
		},
		{
			name:                "Instance with allowed peers only",
			maxPeers:            datagen.RandInt(),
			listenAddr:          datagen.RandHostPort(),
			allowedPeers:        []*enode.Node{testNode(1), testNode(2)},
			cachedPeers:         []*enode.Node{testNode(100)},
			bootstrapNodes:      []*enode.Node{testNode(200)},
			expectedKnownNodes:  nil,
			expectedStaticNodes: []*enode.Node{testNode(1), testNode(2)},
		},
		{
			name:                   "Cached peers append with default fallback nodes",
			maxPeers:               datagen.RandInt(),
			listenAddr:             datagen.RandHostPort(),
			cachedPeers:            []*enode.Node{testNode(2), testNode(3), testNode(4)},
			bootstrapNodes:         nil,
			expectedKnownNodes:     []*enode.Node{testNode(2), testNode(3), testNode(4)},
			expectedDiscoveryNodes: fallbackDiscoveryNodes,
		},
		{
			name:                   "Cached and bootstrap nodes flag are appended",
			maxPeers:               datagen.RandInt(),
			listenAddr:             datagen.RandHostPort(),
			cachedPeers:            []*enode.Node{testNode(2), testNode(22), testNode(222)},
			bootstrapNodes:         []*enode.Node{testNode(3)},
			expectedDiscoveryNodes: []*enode.Node{testNode(3)},
			expectedKnownNodes:     []*enode.Node{testNode(3), testNode(2), testNode(22), testNode(222)},
		},
		{
			name:       "Duplicated nodes are removed (cached and bootstrap nodes) ",
			maxPeers:   datagen.RandInt(),
			listenAddr: datagen.RandHostPort(),
			cachedPeers: []*enode.Node{
				testNode(2),
				testNode(5),
				testNode(2),
				testNode(2),
				testNode(5),
			},
			bootstrapNodes:         []*enode.Node{testNode(3), testNode(33), testNode(33), testNode(3)},
			expectedDiscoveryNodes: []*enode.Node{testNode(3), testNode(33), testNode(33), testNode(3)},
			expectedKnownNodes:     []*enode.Node{testNode(3), testNode(33), testNode(2), testNode(5)},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Instantiate P2P
			thor := New(
				nil,
				privateKey,
				"/tmp/thor-instance",
				nil,
				"1.0",
				tc.maxPeers,
				123,
				tc.listenAddr,
				tc.allowedPeers,
				tc.cachedPeers,
				tc.bootstrapNodes,
			)

			assert.Equal(t, thor.p2pSrv.Options().KnownNodes, tc.expectedKnownNodes)
			assert.Equal(t, thor.p2pSrv.Options().StaticNodes, tc.expectedStaticNodes)
			assert.Equal(t, thor.p2pSrv.Options().DiscoveryNodes, tc.expectedDiscoveryNodes)
			assert.NotNil(t, thor, "P2P instance should not be nil")
			assert.Equal(t, thor.p2pSrv.Options().MaxPeers, tc.maxPeers)
			assert.Equal(t, thor.p2pSrv.Options().ListenAddr, tc.listenAddr)
		})
	}
}
