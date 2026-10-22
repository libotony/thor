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
	// Upstream parked its own socket on 127.0.0.1:0 and recorded that port; the record must
	// advertise the shared discovery socket instead.
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
	if f := s.opts.V5CandidateFilter; f != nil {
		iter = enode.Filter(iter, f)
	}
	s.mix.AddSource(countingIter{enode.NewBufferIter(iter, discoveryPrefetchBuffer), "v5"})
	return nil
}

var discoverableSampleInterval = 5 * time.Minute

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

func (s *Server) discoverableLoop() {
	ticker := time.NewTicker(discoverableSampleInterval)
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
