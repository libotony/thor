// Copyright (c) 2018 The VeChainThor developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package p2psrv

import (
	"fmt"
	"net"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/p2p"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
// and it never dialed. Inbound is the remainder (MaxPeers - dialed).
func TestOutboundQuota(t *testing.T) {
	tests := []struct {
		maxPeers    int
		wantQuota   int
		wantInbound int
	}{
		{0, 0, 0},
		{1, 1, 0},
		{2, 1, 1},
		{3, 1, 2},
		{4, 2, 2},
		{9, 3, 6},
		{25, 5, 20},
	}
	for _, tt := range tests {
		s := New(&Options{MaxPeers: tt.maxPeers})
		assert.Equal(t, tt.wantQuota, s.srv.MaxDialedConns(), "MaxPeers=%d", tt.maxPeers)
		assert.Equal(t, tt.wantInbound, s.srv.MaxInboundConns(), "MaxPeers=%d", tt.maxPeers)
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

	// allowed-peers mode: whitelist becomes static nodes, no UDP socket
	n := fakeNode(7)
	s2 := New(&Options{MaxPeers: 25, NoDiscovery: true, KnownNodes: []*enode.Node{n}})
	assert.True(t, s2.srv.Config.NoDiscovery)
	assert.Equal(t, []*enode.Node{n}, s2.srv.Config.StaticNodes)
	assert.Empty(t, s2.srv.Config.DiscAddr)
}

// TryDial hardcodes upstream's unexported dynDialedConn (1). This pins the static, inbound and
// trusted faces of a wrong flag (trusted would bypass MaxPeers). Flag 0 is indistinguishable
// from outside; it only differs in the dial scheduler's outbound count (dial.go).
func TestTryDial(t *testing.T) {
	newServer := func(peers chan<- *p2p.Peer) *Server {
		key, err := crypto.GenerateKey()
		require.NoError(t, err)
		s := New(&Options{
			Name:        "trydial",
			PrivateKey:  key,
			MaxPeers:    10,
			ListenAddr:  "127.0.0.1:0",
			NoDiscovery: true,
		})
		require.NoError(t, s.Start([]*p2p.Protocol{{
			Name:    "trydial",
			Version: 1,
			Length:  1,
			Run: func(peer *p2p.Peer, rw p2p.MsgReadWriter) error {
				peers <- peer
				for {
					if _, err := rw.ReadMsg(); err != nil {
						return err
					}
				}
			},
		}}, ""))
		t.Cleanup(s.Stop)
		return s
	}

	aPeers, bPeers := make(chan *p2p.Peer, 1), make(chan *p2p.Peer, 1)
	a, b := newServer(aPeers), newServer(bPeers)

	require.NoError(t, a.TryDial(b.Self()))
	out, in := <-aPeers, <-bPeers
	assert.False(t, out.Inbound())
	assert.False(t, out.Info().Network.Static)
	assert.False(t, out.Info().Network.Trusted)
	assert.True(t, in.Inbound())

	// the server dedups already connected nodes
	assert.ErrorIs(t, a.TryDial(b.Self()), p2p.DiscAlreadyConnected)
}

// Upstream ParseV4 accepts DNS names, giving a node without IP. discv5 rejects the whole fallback
// set on one incomplete node, and the peer cache is only rewritten on a clean stop, so a failing
// Start would never recover.
func TestStartToleratesIncompleteKnownNodes(t *testing.T) {
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	pub := crypto.FromECDSAPub(fakeNode(2).Pubkey())[1:]
	incomplete := enode.MustParseV4(fmt.Sprintf("enode://%x@localhost:30303", pub))
	require.Nil(t, incomplete.IP())
	// port 1 on loopback refuses at once, keeping Stop fast
	complete := enode.NewV4(fakeNode(3).Pubkey(), net.IPv4(127, 0, 0, 1), 1, 1)

	s := New(&Options{
		Name:       "incomplete",
		PrivateKey: key,
		MaxPeers:   10,
		ListenAddr: "127.0.0.1:0",
		KnownNodes: []*enode.Node{incomplete, complete},
	})
	require.NoError(t, s.Start(nil, ""))
	t.Cleanup(s.Stop)

	// the complete node still seeds discv5
	require.Len(t, s.bootstrapNodes, 1)
	assert.Equal(t, complete.TCP(), int(s.bootstrapNodes[0].TCP))
}
