// Copyright (c) 2024 The VeChainThor developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package p2p

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"fmt"
	"net"
	"runtime"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/p2p/enr"

	"github.com/vechain/thor/v2/test/datagen"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testNode returns a complete signed node (ip, udp, tcp), so it passes bootnode validation and counts as a dial candidate.
func testNode(b byte) *enode.Node {
	return signedNode(b, true)
}

// enrNode returns a signed ENR carrying ip and udp but no tcp port.
func enrNode(b byte) *enode.Node {
	return signedNode(b, false)
}

func signedNode(b byte, withTCP bool) *enode.Node {
	key, err := crypto.ToECDSA(bytes.Repeat([]byte{b}, 32))
	if err != nil {
		panic(err)
	}
	var r enr.Record
	r.Set(enr.IPv4(net.IPv4(1, 2, 3, b)))
	r.Set(enr.UDP(55555))
	if withTCP {
		r.Set(enr.TCP(11235))
	}
	if err := enode.SignV4(&r, key); err != nil {
		panic(err)
	}
	n, err := enode.New(enode.ValidSchemes, &r)
	if err != nil {
		panic(err)
	}
	return n
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
		staticPeers            []*enode.Node
		cachedPeers            []*enode.Node
		bootstrapNodes         []*enode.Node
		expectedKnownNodes     []*enode.Node
		expectedStaticNodes    []*enode.Node
		expectedDiscoveryNodes []*enode.Node
		expectedNoDiscovery    bool
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
			expectedNoDiscovery: true,
		},
		{
			name:                   "Instance with static peers only keeps discovery",
			maxPeers:               datagen.RandInt(),
			listenAddr:             datagen.RandHostPort(),
			staticPeers:            []*enode.Node{testNode(1), testNode(2)},
			expectedKnownNodes:     nil,
			expectedStaticNodes:    []*enode.Node{testNode(1), testNode(2)},
			expectedDiscoveryNodes: fallbackDiscoveryNodes,
		},
		{
			name:                   "Static, cached and bootstrap nodes stay in their own lists",
			maxPeers:               datagen.RandInt(),
			listenAddr:             datagen.RandHostPort(),
			staticPeers:            []*enode.Node{testNode(1)},
			cachedPeers:            []*enode.Node{testNode(2), testNode(22)},
			bootstrapNodes:         []*enode.Node{testNode(3)},
			expectedKnownNodes:     []*enode.Node{testNode(3), testNode(2), testNode(22)},
			expectedStaticNodes:    []*enode.Node{testNode(1)},
			expectedDiscoveryNodes: []*enode.Node{testNode(3)},
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
			opts, err := BuildOptions(Inputs{
				Version:        "1.0",
				PrivateKey:     privateKey,
				MaxPeers:       tc.maxPeers,
				ListenAddr:     tc.listenAddr,
				AllowedPeers:   tc.allowedPeers,
				StaticPeers:    tc.staticPeers,
				CachedPeers:    tc.cachedPeers,
				BootstrapNodes: tc.bootstrapNodes,
			})
			assert.NoError(t, err)

			assert.Equal(t, opts.KnownNodes, tc.expectedKnownNodes)
			assert.Equal(t, opts.StaticNodes, tc.expectedStaticNodes)
			assert.Equal(t, opts.DiscoveryNodes, tc.expectedDiscoveryNodes)
			assert.Equal(t, opts.NoDiscovery, tc.expectedNoDiscovery)
			assert.NotNil(t, opts, "options should not be nil")
			assert.Equal(t, opts.MaxPeers, tc.maxPeers)
			assert.Equal(t, opts.ListenAddr, tc.listenAddr)
			assert.Equal(t, fmt.Sprintf("thor/v%s/%s/%s", "1.0", runtime.GOOS, runtime.Version()), opts.Name)
		})
	}
}

func TestNewThorP2PAllowedAndStaticExclusive(t *testing.T) {
	privateKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)

	opts, err := BuildOptions(Inputs{
		Version:      "1.0",
		PrivateKey:   privateKey,
		MaxPeers:     10,
		ListenAddr:   datagen.RandHostPort(),
		AllowedPeers: []*enode.Node{testNode(1)},
		StaticPeers:  []*enode.Node{testNode(2)},
	})
	assert.Nil(t, opts)
	assert.ErrorContains(t, err, "mutually exclusive")
}

func TestNewThorP2PBootnodeSplit(t *testing.T) {
	key, _ := crypto.GenerateKey()
	withTCP, noTCP := testNode(1), enrNode(2)
	opts, err := BuildOptions(Inputs{Version: "v", PrivateKey: key, MaxPeers: 10, ListenAddr: ":11235", BootstrapNodes: []*enode.Node{withTCP, noTCP}})
	require.NoError(t, err)
	require.Equal(t, []*enode.Node{withTCP, noTCP}, opts.DiscoveryNodesV5)
	require.Equal(t, []*enode.Node{withTCP, noTCP}, opts.DiscoveryNodes, "shared port: an ENR bootnode also seeds legacy discovery")
	require.Equal(t, []*enode.Node{withTCP}, opts.KnownNodes, "udp-only endpoints are not dial candidates")
}

func TestNewThorP2PDefaultsKeepV5Unseeded(t *testing.T) {
	key, _ := crypto.GenerateKey()
	opts, err := BuildOptions(Inputs{Version: "v", PrivateKey: key, MaxPeers: 10, ListenAddr: ":11235", CachedPeers: []*enode.Node{testNode(3)}})
	require.NoError(t, err)
	require.Empty(t, opts.DiscoveryNodesV5, "fallback list and cached peers never seed v5")
}

func TestNewThorP2PRejectsNoTCPPins(t *testing.T) {
	key, _ := crypto.GenerateKey()
	base := Inputs{Version: "v", PrivateKey: key, MaxPeers: 10, ListenAddr: ":11235"}
	in := base
	in.StaticPeers = []*enode.Node{enrNode(2)}
	_, err := BuildOptions(in)
	require.ErrorContains(t, err, "tcp")
	in = base
	in.AllowedPeers = []*enode.Node{enrNode(2)}
	_, err = BuildOptions(in)
	require.ErrorContains(t, err, "tcp")
}

func TestNewThorP2PRejectsIncompleteBootnode(t *testing.T) {
	key, _ := crypto.GenerateKey()
	nodeKey, _ := crypto.GenerateKey()
	var r enr.Record
	r.Set(enr.UDP(55555))
	require.NoError(t, enode.SignV4(&r, nodeKey))
	n, err := enode.New(enode.ValidSchemes, &r)
	require.NoError(t, err)

	_, err = BuildOptions(Inputs{Version: "v", PrivateKey: key, MaxPeers: 10, ListenAddr: ":11235", BootstrapNodes: []*enode.Node{n}})
	require.ErrorContains(t, err, "bootnode")
}
