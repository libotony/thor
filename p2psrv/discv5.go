// Copyright (c) 2026 The VeChainThor developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package p2psrv

import (
	"fmt"
	"net"

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
	s.mix.AddSource(enode.NewBufferIter(iter, discoveryPrefetchBuffer))
	return nil
}
