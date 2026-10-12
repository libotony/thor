// Copyright (c) 2018 The VeChainThor developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package p2psrv

import (
	"context"
	"errors"
	"math"
	"net"
	"slices"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common/mclock"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/p2p"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/p2p/nat"

	"github.com/vechain/thor/v2/cache"
	"github.com/vechain/thor/v2/log"
	"github.com/vechain/thor/v2/p2p/discv5"
)

var logger = log.WithContext("pkg", "p2psrv")

// Server p2p server wraps ethereum's p2p.Server, and handles discovery v5 stuff.
type Server struct {
	opts           *Options
	srv            *p2p.Server
	discv5         *discv5.Network
	goes           sync.WaitGroup
	done           chan struct{}
	bootstrapNodes []*discv5.Node
	knownNodes     *cache.PrioCache
	candidates     *candidateIter
}

// New create a p2p server.
func New(opts *Options) *Server {
	knownNodes := cache.NewPrioCache(5)
	candidates := newCandidateIter(128)
	for _, node := range opts.KnownNodes {
		knownNodes.Set(node.ID(), node, 0)
		candidates.Add(node)
	}

	cfg := p2p.Config{
		Name:        opts.Name,
		PrivateKey:  opts.PrivateKey,
		MaxPeers:    opts.MaxPeers,
		NoDiscovery: opts.NoDiscovery,
		// Upstream discovery stays off in both modes: peers come from the legacy discv5 topic search
		// (fed through Protocol.DialCandidates) or from StaticNodes.
		DiscoveryV4: false,
		DiscoveryV5: false,
		ListenAddr:  opts.ListenAddr,
		NetRestrict: opts.NetRestrict,
		NAT:         opts.NAT,
		NoDial:      opts.NoDial,
		DialRatio:   max(2, int(math.Sqrt(float64(opts.MaxPeers)))),
	}
	if opts.NoDiscovery {
		// allowed-peers mode: NoDiscovery returns before upstream registers DialCandidates,
		// so the whitelist is dialed (and re-dialed) as static nodes.
		cfg.StaticNodes = opts.KnownNodes
	} else {
		// Upstream binds a UDP socket whenever NoDiscovery is false, and 11235/udp belongs to
		// legacy discv5. Park the unused socket on a random loopback port.
		cfg.DiscAddr = "127.0.0.1:0"
	}

	return &Server{
		opts:       opts,
		srv:        &p2p.Server{Config: cfg},
		done:       make(chan struct{}),
		knownNodes: knownNodes,
		candidates: candidates,
	}
}

// Self returns the local node.
// Only available when server is running.
func (s *Server) Self() *enode.Node {
	return s.srv.Self()
}

// Start start the server.
func (s *Server) Start(protocols []*p2p.Protocol, topic discv5.Topic) error {
	for _, proto := range protocols {
		cpy := *proto
		run := cpy.Run
		cpy.Run = func(peer *p2p.Peer, rw p2p.MsgReadWriter) (err error) {
			dir := "outbound"
			if peer.Inbound() {
				dir = "inbound"
			}
			log := logger.New("peer", peer, "dir", dir)

			log.Trace("peer connected")
			metricConnectedPeers().Add(1)

			startTime := mclock.Now()
			defer func() {
				log.Debug("peer disconnected", "reason", err)
				// Inbound peers carry the remote's ephemeral port, which is useless for redialing.
				if !peer.Inbound() {
					// we assume that good peer has longer connection duration.
					s.knownNodes.Set(peer.ID(), peer.Node(), float64(mclock.Now()-startTime))
				}
				metricConnectedPeers().Add(-1)
			}()
			return run(peer, rw)
		}
		if !s.opts.NoDiscovery {
			// Same iterator on every entry: upstream registers a source once per protocol name,
			// and the iterator supports a single consumer.
			cpy.DialCandidates = s.candidates
		}
		s.srv.Protocols = append(s.srv.Protocols, cpy)
	}

	if err := s.srv.Start(); err != nil {
		return err
	}
	if !s.opts.NoDiscovery {
		if err := s.listenDiscV5(); err != nil {
			return err
		}
		logger.Debug("registering topic", "topic", topic)
		s.goes.Go(func() {
			s.discv5.RegisterTopic(topic, s.done)
		})

		logger.Debug("searching topic", "topic", topic)
		s.goes.Go(func() {
			s.discoverLoop(topic)
		})

		s.goes.Go(s.fetchBootstrap)
	}

	logger.Debug("start up", "self", s.Self())
	return nil
}

// Stop stop the server.
func (s *Server) Stop() {
	if s.discv5 != nil {
		s.discv5.Close()
	}
	s.srv.Stop()
	close(s.done)
	s.goes.Wait()
}

// KnownNodes returns known nodes that can be saved for fast connecting next time.
func (s *Server) KnownNodes() []*enode.Node {
	nodes := make([]*enode.Node, 0, s.knownNodes.Len())
	s.knownNodes.ForEach(func(ent *cache.PrioEntry) bool {
		nodes = append(nodes, ent.Value.(*enode.Node))
		return true
	})
	return nodes
}

// NodeInfo gathers and returns a collection of metadata known about the host.
func (s *Server) NodeInfo() *p2p.NodeInfo {
	return s.srv.NodeInfo()
}

// Options returns the options.
func (s *Server) Options() *Options {
	return s.opts
}

// TryDial dials the node synchronously, bypassing the dial scheduler and its MaxDialedConns
// budget; still bounded by MaxPeers. Kept for external embedders; errors are returned to the caller.
func (s *Server) TryDial(node *enode.Node) error {
	addr, ok := node.TCPEndpoint()
	if !ok {
		return errors.New("node has no TCP endpoint")
	}
	d := &net.Dialer{Timeout: 15 * time.Second}
	conn, err := d.Dial("tcp", addr.String())
	if err != nil {
		return err
	}
	// flag 1 is upstream's unexported dynDialedConn
	return s.srv.SetupConn(conn, 1, node)
}

func (s *Server) listenDiscV5() (err error) {
	// borrowed from ethereum/p2p.Server.Start
	addr, err := net.ResolveUDPAddr("udp", s.opts.ListenAddr)
	if err != nil {
		return err
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return err
	}

	realaddr := conn.LocalAddr().(*net.UDPAddr)
	if s.opts.NAT != nil {
		if !realaddr.IP.IsLoopback() {
			s.goes.Go(func() { nat.Map(s.opts.NAT, s.done, "udp", realaddr.Port, realaddr.Port, "vechain discovery") })
		}
		// TODO: react to external IP changes over time.
		if ext, err := s.opts.NAT.ExternalIP(); err == nil {
			realaddr = &net.UDPAddr{IP: ext, Port: realaddr.Port}
		}
	}

	network, err := discv5.ListenUDP(s.opts.PrivateKey, conn, realaddr, "", s.opts.NetRestrict)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			network.Close()
		}
	}()

	for _, node := range s.opts.DiscoveryNodes {
		s.bootstrapNodes = append(s.bootstrapNodes, toV5Node(node))
	}
	// known nodes are also acting as bootstrap servers
	for _, node := range s.opts.KnownNodes {
		// best effort: discv5 rejects the whole fallback set on one incomplete node
		if _, ok := node.UDPEndpoint(); !ok || node.TCP() == 0 {
			continue
		}
		s.bootstrapNodes = append(s.bootstrapNodes, toV5Node(node))
	}

	if err := network.SetFallbackNodes(s.bootstrapNodes); err != nil {
		return err
	}
	s.discv5 = network
	return nil
}

func (s *Server) discoverLoop(topic discv5.Topic) {
	if s.discv5 == nil {
		return
	}

	setPeriod := make(chan time.Duration, 1)
	setPeriod <- time.Millisecond * 100
	discNodes := make(chan *discv5.Node, 100)
	discLookups := make(chan bool, 100)

	s.goes.Go(func() {
		s.discv5.SearchTopic(topic, setPeriod, discNodes, discLookups)
	})

	var (
		lookupCount  = 0
		fastDiscover = true
		convTime     mclock.AbsTime
	)
	// see go-ethereum serverpool.go
	for {
		select {
		case conv := <-discLookups:
			if conv {
				if lookupCount == 0 {
					convTime = mclock.Now()
				}
				lookupCount++
				if fastDiscover && (lookupCount == 50 || time.Duration(mclock.Now()-convTime) > time.Minute) {
					fastDiscover = false
					setPeriod <- time.Minute
				}
			}
		case v5node := <-discNodes:
			node, err := fromV5Node(v5node)
			if err != nil {
				logger.Debug("discard discovered node", "node", v5node, "err", err)
				continue
			}
			s.candidates.Add(node)
			logger.Trace("discovered node", "node", node)
		case <-s.done:
			close(setPeriod)
			return
		}
	}
}

func (s *Server) fetchBootstrap() {
	if s.opts.RemoteDiscoveryList == "" {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-s.done
		cancel()
	}()

	f := func() error {
		remoteNodes, err := fetchRemoteBootstrapNodes(ctx, s.opts.RemoteDiscoveryList)
		if err != nil {
			return err
		}

		bootnodes := slices.Clone(s.bootstrapNodes)
		bootnodes = append(bootnodes, remoteNodes...)
		if err := s.discv5.SetFallbackNodes(bootnodes); err != nil {
			return err
		}
		return nil
	}

	for {
		err := f()
		if err == nil || errors.Is(err, context.Canceled) {
			return
		}
		logger.Warn("update bootstrap nodes from remote failed", "err", err)

		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second * 10):
		}
	}
}

func toV5Node(n *enode.Node) *discv5.Node {
	return discv5.NewNode(discv5.PubkeyID(n.Pubkey()), n.IP(), uint16(n.UDP()), uint16(n.TCP()))
}

func fromV5Node(n *discv5.Node) (*enode.Node, error) {
	pub, err := crypto.UnmarshalPubkey(append([]byte{4}, n.ID[:]...))
	if err != nil {
		return nil, err
	}
	return enode.NewV4(pub, n.IP, int(n.TCP), int(n.UDP)), nil
}
