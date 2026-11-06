// Copyright (c) 2026 The VeChainThor developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package p2psrv

import (
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/p2p"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/p2p/enr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	smokeWait = 60 * time.Second
	smokeTick = 50 * time.Millisecond
	// An honest peer stops in well under a second; the 30s worst case belongs to malicious peers.
	smokeStopTimeout = 10 * time.Second
)

type smokeNode struct {
	*Server
	peers    chan *p2p.Peer
	stopping atomic.Bool
}

type smokeCfg struct {
	opts Options
	net  uint // advertised "smoke" ENR attribute, lets filters tell test networks apart
}

func withV5Bootnodes(nodes ...*enode.Node) func(*smokeCfg) {
	return func(c *smokeCfg) { c.opts.DiscoveryNodesV5 = nodes }
}

func withV5Filter(f func(*enode.Node) bool) func(*smokeCfg) {
	return func(c *smokeCfg) { c.opts.V5CandidateFilter = f }
}

func withNet(n uint) func(*smokeCfg) {
	return func(c *smokeCfg) { c.net = n }
}

func withOpts(f func(*Options)) func(*smokeCfg) {
	return func(c *smokeCfg) { f(&c.opts) }
}

// smokeFilter accepts nodes advertising the "smoke" attribute with the wanted value.
func smokeFilter(want uint) func(*enode.Node) bool {
	return func(n *enode.Node) bool {
		var got uint
		return n.Load(enr.WithEntry("smoke", &got)) == nil && got == want
	}
}

// startSmoke starts a loopback server running a minimal protocol (two versions, like comm.Protocols)
// whose Run blocks until the peer disconnects. The server is stopped on cleanup.
func startSmoke(t *testing.T, maxPeers int, noDiscovery bool, mods ...func(*smokeCfg)) *smokeNode {
	t.Helper()
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	cfg := smokeCfg{
		opts: Options{
			Name:        "smoke",
			PrivateKey:  key,
			MaxPeers:    maxPeers,
			ListenAddr:  "127.0.0.1:0",
			NoDiscovery: noDiscovery,
		},
		net: 1,
	}
	for _, mod := range mods {
		mod(&cfg)
	}
	n := &smokeNode{
		Server: New(&cfg.opts),
		peers:  make(chan *p2p.Peer, 4),
	}
	run := func(peer *p2p.Peer, rw p2p.MsgReadWriter) error {
		n.peers <- peer
		for {
			if _, err := rw.ReadMsg(); err != nil {
				return err
			}
		}
	}
	attrs := []enr.Entry{enr.WithEntry("smoke", cfg.net)}
	require.NoError(t, n.Start([]*p2p.Protocol{
		{Name: "smoke", Version: 1, Length: 1, Run: run, Attributes: attrs},
		{Name: "smoke", Version: 2, Length: 1, Run: run, Attributes: attrs},
	}, "smoke"))
	t.Cleanup(func() { n.stopWithin(t, smokeStopTimeout) })
	return n
}

// stopWithin stops the server once and fails the test if Stop hangs.
func (n *smokeNode) stopWithin(t *testing.T, d time.Duration) {
	t.Helper()
	if !n.stopping.CompareAndSwap(false, true) {
		return
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		n.Stop()
	}()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("Stop did not return within %s", d)
	}
}

func (n *smokeNode) recvPeer(t *testing.T) *p2p.Peer {
	t.Helper()
	select {
	case p := <-n.peers:
		return p
	case <-time.After(smokeStopTimeout):
		t.Fatal("protocol Run was not invoked")
		return nil
	}
}

func (n *smokeNode) peerCount() int { return n.srv.PeerCount() }

// Two main-mode servers connect through the discovery path (candidates -> iterator -> dial
// scheduler), which is the only behavioural coverage of the cpy.DialCandidates wiring in Start.
// Stopping with live peers pins the iterator Close -> dial scheduler shutdown chain, and the
// post-stop KnownNodes pin the warm-start contract: outbound peers are recorded with the real
// listening port, inbound peers (ephemeral remote port) are not.
func TestSmokeInterconnect(t *testing.T) {
	a, b := startSmoke(t, 10, false), startSmoke(t, 10, false)
	bPort := b.NodeInfo().Ports.Listener
	require.NotZero(t, bPort)

	a.candidates.Add(b.Self())
	require.Eventually(t, func() bool {
		return a.peerCount() == 1 && b.peerCount() == 1
	}, smokeWait, smokeTick)

	out, in := a.recvPeer(t), b.recvPeer(t)
	assert.False(t, out.Inbound())
	assert.True(t, in.Inbound())

	a.stopWithin(t, smokeStopTimeout)
	b.stopWithin(t, smokeStopTimeout)

	known := a.KnownNodes()
	require.Len(t, known, 1)
	assert.Equal(t, b.Self().ID(), known[0].ID())
	assert.Equal(t, bPort, known[0].TCP())
	assert.Empty(t, b.KnownNodes())
}

// Every protocol entry shares the server's candidate mix in main mode; allowed-peers mode
// returns before upstream registers DialCandidates, so none must be set.
func TestSmokeDialCandidatesWiring(t *testing.T) {
	mainMode := startSmoke(t, 10, false)
	require.Len(t, mainMode.srv.Protocols, 2)
	for _, p := range mainMode.srv.Protocols {
		assert.Same(t, mainMode.mix, p.DialCandidates, "version %d", p.Version)
	}

	allowed := startSmoke(t, 10, true)
	require.Len(t, allowed.srv.Protocols, 2)
	for _, p := range allowed.srv.Protocols {
		assert.Nil(t, p.DialCandidates, "version %d", p.Version)
	}
}

func TestSmokeV5Discovery(t *testing.T) {
	// b learns a from a's ping and may dial back; it must carry the same filter so it never dials c.
	b := startSmoke(t, 10, false, withV5Filter(smokeFilter(1)))
	// c is on the wrong network: visible in a's table, never dialed. NoDial keeps c from dialing a
	// (it has no filter) so peer counts stay deterministic.
	c := startSmoke(t, 10, false, withNet(2), withOpts(func(o *Options) { o.NoDial = true }))
	var rejected atomic.Int32
	filter := func(n *enode.Node) bool {
		ok := smokeFilter(1)(n)
		if !ok {
			rejected.Add(1)
		}
		return ok
	}
	a := startSmoke(t, 10, false,
		withV5Bootnodes(b.LocalNode().Node(), c.LocalNode().Node()),
		withV5Filter(filter))

	// legacy discovery has no bootstrap here and KnownNodes is empty: v5 is the only path to b
	require.Eventually(t, func() bool { return a.peerCount() == 1 && b.peerCount() == 1 }, smokeWait, smokeTick)
	require.Eventually(t, func() bool { return rejected.Load() > 0 }, smokeWait, smokeTick)
	require.Never(t, func() bool { return c.peerCount() > 0 }, time.Second, smokeTick)
	bID := b.Self().ID()
	require.Eventually(t, func() bool {
		return slices.ContainsFunc(a.v5.AllNodes(), func(n *enode.Node) bool { return n.ID() == bID })
	}, smokeWait, smokeTick)
	require.Eventually(t, func() bool { return a.countDiscoverable() == 1 }, smokeWait, smokeTick)

	a.stopWithin(t, smokeStopTimeout) // v5 lookups in flight: pins the legacy→v5→server order
}

// With every inbound slot taken, extra inbound connections are refused while the outbound
// budget stays usable: MaxPeers 4 -> MaxDialedConns 2, MaxInboundConns 2.
// Loopback counts as LAN, so upstream's 30s per-IP inbound throttle does not apply.
func TestInboundCapLeavesRoomToDial(t *testing.T) {
	a := startSmoke(t, 4, false)
	require.Equal(t, 2, a.srv.MaxDialedConns())
	require.Equal(t, 2, a.srv.MaxInboundConns())

	for range a.srv.MaxInboundConns() {
		x := startSmoke(t, 10, true)
		require.NoError(t, x.TryDial(a.Self()))
	}
	require.Eventually(t, func() bool { return a.peerCount() == 2 }, smokeWait, smokeTick)

	extra := startSmoke(t, 10, true)
	require.ErrorIs(t, extra.TryDial(a.Self()), p2p.DiscTooManyPeers)
	assert.Equal(t, 2, a.peerCount())

	// outbound goes through the dial scheduler, fed by the discovery candidates
	y := startSmoke(t, 10, true)
	a.candidates.Add(y.Self())
	require.Eventually(t, func() bool {
		return a.peerCount() == 3 && y.peerCount() == 1
	}, smokeWait, smokeTick)

	var outbound []enode.ID
	for _, p := range a.srv.Peers() {
		if !p.Inbound() {
			outbound = append(outbound, p.ID())
		}
	}
	assert.Equal(t, []enode.ID{y.Self().ID()}, outbound)
}
