// Copyright (c) 2018 The VeChainThor developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package p2psrv

import (
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/stretchr/testify/assert"
)

func TestNewServer(t *testing.T) {
	privateKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("Unable to generate private key: %v", err)
	}

	node := fakeNode(1)
	opts := &Options{
		Name:        "testNode",
		PrivateKey:  privateKey,
		MaxPeers:    10,
		ListenAddr:  ":30303",
		NetRestrict: nil,
		NAT:         nil,
		NoDial:      false,
		KnownNodes:  []*enode.Node{node},
	}

	server := New(opts)

	assert.Equal(t, "testNode", server.opts.Name)
	assert.Equal(t, privateKey, server.opts.PrivateKey)
	assert.Equal(t, 10, server.opts.MaxPeers)
	assert.Equal(t, ":30303", server.opts.ListenAddr)
	assert.Equal(t, server.candidates.cache.Len(), 1)
	assert.Equal(t, server.knownNodes.Len(), 1)
	assert.True(t, server.candidates.cache.Contains(node.ID()))
	assert.True(t, server.knownNodes.Contains(node.ID()))
	assert.False(t, server.opts.NoDial)
}

func TestNewServerConnectOnly(t *testing.T) {
	privateKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("Unable to generate private key: %v", err)
	}

	knownNode := fakeNode(1)
	opts := &Options{
		Name:        "testNode",
		PrivateKey:  privateKey,
		MaxPeers:    10,
		ListenAddr:  ":30303",
		NetRestrict: nil,
		NAT:         nil,
		NoDial:      false,
		KnownNodes:  []*enode.Node{knownNode},
	}

	server := New(opts)

	assert.Equal(t, "testNode", server.opts.Name)
	assert.Equal(t, privateKey, server.opts.PrivateKey)
	assert.Equal(t, 10, server.opts.MaxPeers)
	assert.Equal(t, ":30303", server.opts.ListenAddr)
	assert.False(t, server.opts.NoDial)

	assert.Equal(t, server.candidates.cache.Len(), 1)
	assert.Equal(t, server.knownNodes.Len(), 1)
	assert.True(t, server.candidates.cache.Contains(knownNode.ID()))
	assert.True(t, server.knownNodes.Contains(knownNode.ID()))
}

// int(sqrt(MaxPeers)) rounds down to 1 for MaxPeers 1..3, which would give outbound
// every slot and leave the inbound cap at 0. MaxPeers >= 4 keeps the plain sqrt.
// The {25, 5} row is p2psrv's side of the 5 slots upstream reserves
// (see TestOutboundQuota for the resulting MaxDialedConns).
func TestDialRatioFloor(t *testing.T) {
	tests := []struct {
		maxPeers  int
		wantRatio int
	}{
		{0, 2},
		{1, 2},
		{2, 2},
		{3, 2},
		{4, 2},
		{9, 3},
		{25, 5},
	}
	for _, tt := range tests {
		s := New(&Options{MaxPeers: tt.maxPeers})
		assert.Equal(t, tt.wantRatio, s.srv.DialRatio, "MaxPeers=%d", tt.maxPeers)
	}
}

// The dial cap upstream derives from DialRatio must keep the slots T8 reserves.
// MaxPeers 1 is the case that broke: DialRatio is floored at 2, so 1/2 truncated to 0
// and it never dialed.
func TestOutboundQuota(t *testing.T) {
	tests := []struct {
		maxPeers  int
		wantQuota int
	}{
		{0, 0},
		{1, 1},
		{2, 1},
		{3, 1},
		{4, 2},
		{9, 3},
		{25, 5},
	}
	for _, tt := range tests {
		s := New(&Options{MaxPeers: tt.maxPeers})
		assert.Equal(t, tt.wantQuota, s.srv.MaxDialedConns(), "MaxPeers=%d", tt.maxPeers)
	}
}

// Upstream registers Protocol.DialCandidates only when NoDiscovery is false (and binds a UDP
// socket in that case), so the two modes map to different Config shapes.
func TestConfigModes(t *testing.T) {
	// main mode: upstream discovery off, idle UDP socket parked on loopback, no static nodes
	s := New(&Options{MaxPeers: 25})
	cfg := s.srv.Config
	assert.False(t, cfg.NoDiscovery)
	assert.False(t, cfg.DiscoveryV4)
	assert.False(t, cfg.DiscoveryV5)
	assert.Equal(t, "127.0.0.1:0", cfg.DiscAddr)
	assert.Empty(t, cfg.StaticNodes)

	// allowed-peers mode: whitelist is passed through as static nodes, no UDP socket
	n := fakeNode(7)
	s2 := New(&Options{MaxPeers: 25, NoDiscovery: true, StaticNodes: []*enode.Node{n}})
	assert.True(t, s2.srv.Config.NoDiscovery)
	assert.Equal(t, []*enode.Node{n}, s2.srv.Config.StaticNodes)
	assert.Empty(t, s2.srv.Config.DiscAddr)

	// KnownNodes is a warm-start cache and must never be promoted to static nodes
	s3 := New(&Options{MaxPeers: 25, NoDiscovery: true, KnownNodes: []*enode.Node{n}})
	assert.Empty(t, s3.srv.Config.StaticNodes)
}
