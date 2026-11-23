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
	"github.com/ethereum/go-ethereum/p2p/discover"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/p2p/nat"

	"github.com/vechain/thor/v2/cache"
	"github.com/vechain/thor/v2/log"
	"github.com/vechain/thor/v2/p2p/discv5"
)

var logger = log.WithContext("pkg", "p2psrv")

// Variables so tests can shorten them.
var (
	// remoteListTimeout bounds the fetch of the bootnode list in Start.
	remoteListTimeout = 3 * time.Second
	// remoteListRetryInterval is the pause between background fetches after a failed start-up fetch.
	remoteListRetryInterval = 10 * time.Second
)

const (
	// candidateMixTimeout is how long FairMix waits on the source whose turn it is before taking a candidate from any source
	candidateMixTimeout = 100 * time.Millisecond
	// prefetchNodeCount is how many filtered discv5 candidates are buffered ahead of the dialer
	prefetchNodeCount = 32
)

// Application is everything the server needs from the protocol layer running on top of it.
// Start reads Protocols and DiscTopic once, before it returns, and rejects a nil Application.
type Application interface {
	// Protocols returns the devp2p protocols served on every connection.
	Protocols() []*p2p.Protocol
	// DiscTopic returns the legacy discovery topic to register and search.
	DiscTopic() discv5.Topic
	// AcceptV5Candidate reports whether a node found by modern discv5 may be dialed. A connected
	// peer it accepts also counts as discoverable in p2p_v5_discoverable_peers_gauge, even when
	// that peer is not in the discv5 table. It is called from the discovery iterator goroutine
	// and the discoverability sampler, so it must be safe for concurrent use.
	AcceptV5Candidate(*enode.Node) bool
}

// Server p2p server wraps ethereum's p2p.Server, and handles discovery v5 stuff.
type Server struct {
	opts           *Options
	app            Application
	srv            *p2p.Server
	legacy         *discv5.Network
	goes           sync.WaitGroup
	done           chan struct{}
	bootstrapNodes []*discv5.Node
	knownNodes     *cache.PrioCache
	candidates     *candidateIter
	v5             *discover.UDPv5
	mix            *enode.FairMix
	// v5Bootnodes is what was handed to discover.ListenV5.
	v5Bootnodes []*enode.Node

	remoteMu     sync.Mutex
	remoteLegacy []*enode.Node
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
		StaticNodes: opts.StaticNodes,
	}
	if !opts.NoDiscovery {
		// Upstream binds a UDP socket whenever NoDiscovery is false, but discovery runs on a
		// socket this package opens itself (shared with the legacy stack unless that is off).
		// Park the unused upstream socket on a random loopback port.
		cfg.DiscAddr = "127.0.0.1:0"
	}

	return &Server{
		opts:       opts,
		srv:        &p2p.Server{Config: cfg},
		done:       make(chan struct{}),
		knownNodes: knownNodes,
		candidates: candidates,
		mix:        enode.NewFairMix(candidateMixTimeout),
	}
}

// Self returns the local node.
// Only available when server is running.
func (s *Server) Self() *enode.Node {
	return s.srv.Self()
}

// LocalNode returns the local node record handle.
// Only available when server is running.
func (s *Server) LocalNode() *enode.LocalNode {
	return s.srv.LocalNode()
}

// Start start the server.
func (s *Server) Start(app Application) error {
	if app == nil {
		return errors.New("p2psrv: nil application")
	}
	s.app = app
	topic := app.DiscTopic()

	// Modern discv5 only takes seeds at start-up, so the list is fetched before anything else comes up.
	s.v5Bootnodes = slices.Clone(s.opts.DiscoveryNodesV5)
	fetchFailed := false
	if !s.opts.NoDiscovery && s.opts.RemoteDiscoveryList != "" {
		ctx, cancel := context.WithTimeout(context.Background(), remoteListTimeout)
		nodes, err := fetchBootnodeList(ctx, s.opts.RemoteDiscoveryList)
		cancel()
		if err != nil {
			logger.Warn("fetch bootnode list failed, continuing with built-in seeds", "err", err)
			fetchFailed = true
		} else {
			s.setRemoteLegacy(nodes)
			s.v5Bootnodes = append(s.v5Bootnodes, nodes...)
		}
	}
	for _, proto := range app.Protocols() {
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
			metricPeersByDir().AddWithLabel(1, map[string]string{"dir": dir})

			startTime := mclock.Now()
			defer func() {
				log.Debug("peer disconnected", "reason", err)
				// Inbound peers carry the remote's ephemeral port, which is useless for redialing.
				if !peer.Inbound() {
					// we assume that good peer has longer connection duration.
					s.knownNodes.Set(peer.ID(), peer.Node(), float64(mclock.Now()-startTime))
				}
				metricConnectedPeers().Add(-1)
				metricPeersByDir().AddWithLabel(-1, map[string]string{"dir": dir})
			}()
			return run(peer, rw)
		}
		if !s.opts.NoDiscovery {
			// Same iterator on every entry: upstream registers a source once per protocol name,
			// and the iterator supports a single consumer.
			cpy.DialCandidates = s.mix
		}
		s.srv.Protocols = append(s.srv.Protocols, cpy)
	}

	if err := s.srv.Start(); err != nil {
		return err
	}
	if !s.opts.NoDiscovery {
		// Added here rather than in New: AddSource starts a goroutine that calls Next() on the
		// source right away, taking one node out of the candidate cache and holding it until the
		// dial scheduler (which only exists after srv.Start) consumes it. Done in New, a seeded
		// KnownNode would already be gone from the cache before Start.
		// The source stays attached when the legacy stack is off: New seeds s.candidates with
		// KnownNodes, and this is their only path to the dialer.
		s.mix.AddSource(countingIter{s.candidates, "legacy"})
		if err := s.listenDiscovery(); err != nil {
			return err
		}
		s.goes.Go(s.discoveryMetricsLoop)
		if s.legacy != nil {
			logger.Debug("registering topic", "topic", topic)
			s.goes.Go(func() {
				s.legacy.RegisterTopic(topic, s.done)
			})

			logger.Debug("searching topic", "topic", topic)
			s.goes.Go(func() {
				s.legacyDiscoverLoop(topic)
			})

			if fetchFailed {
				s.goes.Go(s.retryRemoteList)
			}
		}
	}

	self := s.Self()
	logger.Debug("start up", "self", self.URLv4(), "enr", self.String())
	return nil
}

// Stop stop the server.
func (s *Server) Stop() {
	// Order matters: legacy Close closes the socket, whose read loop then closes the unhandled
	// channel; v5.Close waits for its read loop, which only returns once that channel is closed.
	if s.legacy != nil {
		s.legacy.Close()
	}
	if s.v5 != nil {
		s.v5.Close()
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

func (s *Server) listenDiscovery() (err error) {
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
			s.srv.LocalNode().SetStaticIP(ext)
		}
	}

	if s.opts.NoLegacyDiscovery {
		// v5 owns the socket: v5.Close closes it, so Stop has no ordering constraint.
		if err := s.listenV5(conn, nil); err != nil {
			conn.Close()
			return err
		}
		return nil
	}

	unhandled := make(chan discover.ReadPacket, 100)
	network, err := discv5.ListenUDP(s.opts.PrivateKey, conn, realaddr, "", s.opts.NetRestrict, unhandled)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			network.Close()
		}
	}()

	for _, node := range s.opts.DiscoveryNodes {
		s.bootstrapNodes = append(s.bootstrapNodes, toLegacyNode(node))
	}
	// known nodes are also acting as bootstrap servers
	for _, node := range s.opts.KnownNodes {
		// best effort: discv5 rejects the whole fallback set on one incomplete node
		if _, ok := node.UDPEndpoint(); !ok || node.TCP() == 0 {
			continue
		}
		s.bootstrapNodes = append(s.bootstrapNodes, toLegacyNode(node))
	}

	applied, err := setLegacyFallback(network, s.bootstrapNodes, s.remoteLegacyNodes())
	if err != nil {
		return err
	}
	s.bootstrapNodes = applied
	if err := s.listenV5(conn, unhandled); err != nil {
		return err
	}
	s.legacy = network
	return nil
}

// setLegacyFallback seeds the legacy stack with base plus the remote nodes. The legacy stack rejects the
// whole set on one bad node and the remote file is untrusted, so on rejection it falls back to base alone.
// It returns the set that was applied.
func setLegacyFallback(network *discv5.Network, base []*discv5.Node, remote []*enode.Node) ([]*discv5.Node, error) {
	if len(remote) > 0 {
		all := slices.Clone(base)
		for _, node := range remote {
			all = append(all, toLegacyNode(node))
		}
		err := network.SetFallbackNodes(all)
		if err == nil {
			return all, nil
		}
		logger.Warn("legacy stack rejected the seed set that includes remote bootnodes, retrying without them", "err", err)
	}
	return base, network.SetFallbackNodes(base)
}

func (s *Server) legacyDiscoverLoop(topic discv5.Topic) {
	if s.legacy == nil {
		return
	}

	setPeriod := make(chan time.Duration, 1)
	setPeriod <- time.Millisecond * 100
	discNodes := make(chan *discv5.Node, 100)
	discLookups := make(chan bool, 100)

	s.goes.Go(func() {
		s.legacy.SearchTopic(topic, setPeriod, discNodes, discLookups)
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
		case legacyNode := <-discNodes:
			node, err := fromLegacyNode(legacyNode)
			if err != nil {
				logger.Debug("discard discovered node", "node", legacyNode, "err", err)
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

func (s *Server) setRemoteLegacy(nodes []*enode.Node) {
	s.remoteMu.Lock()
	defer s.remoteMu.Unlock()
	s.remoteLegacy = nodes
}

func (s *Server) remoteLegacyNodes() []*enode.Node {
	s.remoteMu.Lock()
	defer s.remoteMu.Unlock()
	return s.remoteLegacy
}

// retryRemoteList runs after a failed start-up fetch and updates the legacy stack only.
func (s *Server) retryRemoteList() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		select {
		case <-s.done:
			cancel()
		case <-ctx.Done():
		}
	}()

	for {
		select {
		case <-s.done:
			return
		case <-time.After(remoteListRetryInterval):
		}

		actx, acancel := context.WithTimeout(ctx, remoteListTimeout)
		remote, err := fetchBootnodeList(actx, s.opts.RemoteDiscoveryList)
		acancel()
		if err != nil {
			logger.Warn("update bootstrap nodes from remote failed", "err", err)
			continue
		}
		bootnodes := slices.Clone(s.bootstrapNodes)
		for _, node := range remote {
			bootnodes = append(bootnodes, toLegacyNode(node))
		}
		if err := s.legacy.SetFallbackNodes(bootnodes); err != nil {
			logger.Warn("update bootstrap nodes from remote failed", "err", err)
			continue
		}
		s.setRemoteLegacy(remote)
		return
	}
}

func toLegacyNode(n *enode.Node) *discv5.Node {
	// Shared-port bootnodes (disco) advertise tcp=udp in legacy discovery, so a tcp-less ENR maps to
	// that same endpoint; legacy validateComplete would otherwise reject the whole fallback set.
	tcp := n.TCP()
	if tcp == 0 {
		tcp = n.UDP()
	}
	return discv5.NewNode(discv5.PubkeyID(n.Pubkey()), n.IP(), uint16(n.UDP()), uint16(tcp))
}

func fromLegacyNode(n *discv5.Node) (*enode.Node, error) {
	pub, err := crypto.UnmarshalPubkey(append([]byte{4}, n.ID[:]...))
	if err != nil {
		return nil, err
	}
	return enode.NewV4(pub, n.IP, int(n.TCP), int(n.UDP)), nil
}
