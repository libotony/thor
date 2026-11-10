// Copyright (c) 2026 The VeChainThor developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package p2psrv

import (
	"fmt"
	"net"
	"time"

	"github.com/ethereum/go-ethereum/p2p/discover"
	"github.com/ethereum/go-ethereum/p2p/enode"
)

// V5ProtocolID replaces the upstream "discv5" packet header id, isolating the
// VeChain DHT from Ethereum's at the wire level: a mismatched id fails v5wire
// header validation and the packet is dropped before any handshake.
var V5ProtocolID = [6]byte{'v', 'e', 'c', 'h', 'n', '5'}

func (s *Server) listenV5(conn *net.UDPConn, unhandled chan discover.ReadPacket) error {
	ln := s.srv.LocalNode()
	// Upstream binds its own UDP socket whenever NoDiscovery is false, and we need that
	// mode for DialCandidates to be wired in. Since 11235/udp already belongs to the legacy
	// stack, New parks that socket on 127.0.0.1:0 (a random loopback port nobody talks to),
	// and upstream then records the parked port as the ENR udp fallback. Override it with
	// the port of the shared discovery socket, which is what peers must reach us on.
	ln.SetFallbackUDP(conn.LocalAddr().(*net.UDPAddr).Port)

	v5, err := discover.ListenV5(&discover.SharedUDPConn{UDPConn: conn, Unhandled: unhandled}, ln, discover.Config{
		PrivateKey:   s.opts.PrivateKey,
		NetRestrict:  s.opts.NetRestrict,
		Bootnodes:    s.opts.DiscoveryNodesV5,
		V5ProtocolID: &V5ProtocolID,
	})
	if err != nil {
		return fmt.Errorf("listen discv5: %w", err)
	}
	s.v5 = v5

	iter := v5.RandomNodes()
	// The filter is the protocol layer's compatibility judgment (same rule as the Status
	// handshake). Rejected nodes are simply not dialed; they stay in the table and keep
	// serving lookups.
	if f := s.opts.V5CandidateFilter; f != nil {
		iter = enode.Filter(iter, f)
	}
	s.mix.AddSource(countingIter{enode.NewBufferIter(iter, v5PrefetchNodeCount), "v5"})
	return nil
}

var discoveryMetricsInterval = 5 * time.Minute

// countDiscoverable returns how many connected peers the modern discv5 stack can find: peers in
// its table, or peers the candidate filter would accept.
func (s *Server) countDiscoverable() int {
	if s.v5 == nil {
		return 0
	}
	inTable := make(map[enode.ID]struct{})
	for _, n := range s.v5.AllNodes() {
		inTable[n.ID()] = struct{}{}
	}
	count := 0
	for _, p := range s.srv.Peers() {
		if _, ok := inTable[p.ID()]; ok {
			count++
		} else if f := s.opts.V5CandidateFilter; f != nil && f(p.Node()) {
			count++
		}
	}
	return count
}

// discoveryMetricsLoop samples how many connected peers are reachable through modern discv5 and publishes the gauge every discoveryMetricsInterval until Stop.
func (s *Server) discoveryMetricsLoop() {
	ticker := time.NewTicker(discoveryMetricsInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			metricV5DiscoverablePeers().Set(int64(s.countDiscoverable()))
		case <-s.done:
			return
		}
	}
}
