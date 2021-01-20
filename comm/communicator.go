// Copyright (c) 2018 The VeChainThor developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package comm

import (
	"context"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/p2p"
	"github.com/inconshreveable/log15"
	"github.com/vechain/thor/block"
	"github.com/vechain/thor/chain"
	"github.com/vechain/thor/co"
	"github.com/vechain/thor/comm/proto"
	"github.com/vechain/thor/p2psrv/discv5"
	"github.com/vechain/thor/thor"
	"github.com/vechain/thor/tx"
	"github.com/vechain/thor/txpool"
)

var log = log15.New("pkg", "comm")

// Communicator communicates with remote p2p peers to exchange blocks and txs, etc.
type Communicator struct {
	repo            *chain.Repository
	txPool          *txpool.TxPool
	ctx             context.Context
	cancel          context.CancelFunc
	peerSet         *PeerSet
	syncedCh        chan struct{}
	newBlockFeed    event.Feed
	newDraftFeed    event.Feed
	newAcceptedFeed event.Feed
	announcementCh  chan *announcement
	feedScope       event.SubscriptionScope
	goes            co.Goes
	onceSynced      sync.Once
}

// New create a new Communicator instance.
func New(repo *chain.Repository, txPool *txpool.TxPool) *Communicator {
	ctx, cancel := context.WithCancel(context.Background())
	return &Communicator{
		repo:           repo,
		txPool:         txPool,
		ctx:            ctx,
		cancel:         cancel,
		peerSet:        newPeerSet(),
		syncedCh:       make(chan struct{}),
		announcementCh: make(chan *announcement),
	}
}

// Synced returns a channel indicates if synchronization process passed.
func (c *Communicator) Synced() <-chan struct{} {
	return c.syncedCh
}

// Sync start synchronization process.
func (c *Communicator) Sync(handler HandleBlockStream) {
	const initSyncInterval = 2 * time.Second
	const syncInterval = 30 * time.Second

	c.goes.Go(func() {
		timer := time.NewTimer(0)
		defer timer.Stop()
		delay := initSyncInterval
		syncCount := 0

		shouldSynced := func() bool {
			bestBlockTime := c.repo.BestBlock().Header().Timestamp()
			now := uint64(time.Now().Unix())
			if bestBlockTime+thor.BlockInterval >= now {
				return true
			}
			if syncCount > 2 {
				return true
			}
			return false
		}

		for {
			timer.Stop()
			timer = time.NewTimer(delay)
			select {
			case <-c.ctx.Done():
				return
			case <-timer.C:
				log.Debug("synchronization start")

				best := c.repo.BestBlock().Header()
				// choose peer which has the head block with higher total score
				peer := c.peerSet.Slice().Find(func(peer *Peer) bool {
					_, totalScore := peer.Head()
					return totalScore >= best.TotalScore()
				})
				if peer == nil {
					if c.peerSet.Len() < 3 {
						log.Debug("no suitable peer to sync")
						break
					}
					// if more than 3 peers connected, we are assumed to be the best
					log.Debug("synchronization done, best assumed")
				} else {
					if err := c.sync(peer, best.Number(), handler); err != nil {
						peer.logger.Debug("synchronization failed", "err", err)
						break
					}
					peer.logger.Debug("synchronization done")
				}
				syncCount++

				if shouldSynced() {
					delay = syncInterval
					c.onceSynced.Do(func() {
						close(c.syncedCh)
					})
				}
			}
		}
	})
}

// Protocols returns all supported protocols.
func (c *Communicator) Protocols() []*p2p.Protocol {
	return []*p2p.Protocol{
		{
			Name:    proto.Name,
			Version: 1,
			Length:  8,
			Run:     c.servePeer,
		}, {
			Name:    proto.Name,
			Version: proto.Version,
			Length:  proto.Length,
			Run:     c.servePeer,
		},
	}
}

// DiscTopic  returns the topic for p2p network discovery.
func (c *Communicator) DiscTopic() discv5.Topic {
	genesisID := c.repo.GenesisBlock().Header().ID()
	return discv5.Topic(fmt.Sprintf("thor1@%x", genesisID[24:]))
}

// Start start the communicator.
func (c *Communicator) Start() {
	c.goes.Go(c.txsLoop)
	c.goes.Go(c.announcementLoop)
}

// Stop stop the communicator.
func (c *Communicator) Stop() {
	c.cancel()
	c.feedScope.Close()
	c.goes.Wait()
}

type txsToSync struct {
	txs    tx.Transactions
	synced bool
}

func (c *Communicator) servePeer(p *p2p.Peer, rw p2p.MsgReadWriter) error {
	peer := newPeer(p, rw)
	c.goes.Go(func() {
		c.runPeer(peer)
	})

	var txsToSync txsToSync

	return peer.Serve(func(msg *p2p.Msg, w func(interface{})) error {
		return c.handleRPC(peer, msg, w, &txsToSync)
	}, proto.MaxMsgSize)
}

func (c *Communicator) runPeer(peer *Peer) {
	defer peer.Disconnect(p2p.DiscRequested)

	// 5sec timeout for handshake
	ctx, cancel := context.WithTimeout(c.ctx, time.Second*5)
	defer cancel()

	status, err := proto.GetStatus(ctx, peer)
	if err != nil {
		peer.logger.Debug("failed to get status", "err", err)
		return
	}
	if status.GenesisBlockID != c.repo.GenesisBlock().Header().ID() {
		peer.logger.Debug("failed to handshake", "err", "genesis id mismatch")
		return
	}
	localClock := uint64(time.Now().Unix())
	remoteClock := status.SysTimestamp

	diff := localClock - remoteClock
	if localClock < remoteClock {
		diff = remoteClock - localClock
	}
	if diff > thor.BlockInterval*2 {
		peer.logger.Debug("failed to handshake", "err", "sys time diff too large")
		return
	}

	peer.UpdateHead(status.BestBlockID, status.TotalScore)
	c.peerSet.Add(peer)
	peer.logger.Debug(fmt.Sprintf("peer added (%v)", c.peerSet.Len()))

	defer func() {
		c.peerSet.Remove(peer.ID())
		peer.logger.Debug(fmt.Sprintf("peer removed (%v)", c.peerSet.Len()))
	}()

	select {
	case <-peer.Done():
	case <-c.ctx.Done():
	case <-c.syncedCh:
		c.syncTxs(peer)
		select {
		case <-peer.Done():
		case <-c.ctx.Done():
		}
	}
}

// SubscribeBlock subscribe the event that new block received.
func (c *Communicator) SubscribeBlock(ch chan *NewBlockEvent) event.Subscription {
	return c.feedScope.Track(c.newBlockFeed.Subscribe(ch))
}

// BroadcastBlock broadcast a block to remote peers.
func (c *Communicator) BroadcastBlock(blk *block.Block) {
	peers := c.peerSet.Slice().Filter(func(p *Peer) bool {
		return !p.IsBlockKnown(blk.Header().ID())
	})

	p := int(math.Sqrt(float64(len(peers))))
	toPropagate := peers[:p]
	toAnnounce := peers[p:]

	for _, peer := range toPropagate {
		peer := peer
		peer.MarkBlock(blk.Header().ID())
		c.goes.Go(func() {
			if err := proto.NotifyNewBlock(c.ctx, peer, blk); err != nil {
				peer.logger.Debug("failed to broadcast new block", "err", err)
			}
		})
	}

	for _, peer := range toAnnounce {
		peer := peer
		peer.MarkBlock(blk.Header().ID())
		c.goes.Go(func() {

			if err := proto.NotifyNewBlockID(c.ctx, peer, blk.Header().ID()); err != nil {
				peer.logger.Debug("failed to broadcast new block id", "err", err)
			}
		})
	}
}

// PeerCount returns count of peers.
func (c *Communicator) PeerCount() int {
	return c.peerSet.Len()
}

// PeersStats returns all peers' stats
func (c *Communicator) PeersStats() []*PeerStats {
	var stats []*PeerStats
	for _, peer := range c.peerSet.Slice() {
		bestID, totalScore := peer.Head()
		stats = append(stats, &PeerStats{
			Name:        peer.Name(),
			BestBlockID: bestID,
			TotalScore:  totalScore,
			PeerID:      peer.ID().String(),
			NetAddr:     peer.RemoteAddr().String(),
			Inbound:     peer.Inbound(),
			Duration:    uint64(time.Duration(peer.Duration()) / time.Second),
		})
	}
	sort.Slice(stats, func(i, j int) bool {
		return stats[i].Duration < stats[j].Duration
	})
	return stats
}

// BroadcastDraft broadcast a draft to remote peers.
func (c *Communicator) BroadcastDraft(d *proto.Draft, local bool) {
	hash := d.Hash()

	peers := c.peerSet.Slice().Filter(func(peer *Peer) bool {
		// MsgNewDraft was introduced in protocol 'thor/2'
		return peer.TestCap(2) && !peer.IsDraftKnown(hash)
	})

	for _, peer := range peers {
		peer := peer
		peer.MarkDraft(hash)
		c.goes.Go(func() {
			if err := proto.NotifyNewDraft(c.ctx, peer, d); err != nil {
				peer.logger.Debug("failed to broadcast new draft", "err", err)
			}
		})
	}

	if local {
		c.goes.Go(func() {
			// draft is generated locally, broadcast to notify backer loop
			c.newDraftFeed.Send(&NewDraftEvent{
				Draft: d,
			})
		})
	}
}

// BroadcastAccepted broadcast an accepted message to remote peers.
func (c *Communicator) BroadcastAccepted(acc *proto.Accepted, local bool) {
	peers := c.peerSet.Slice().Filter(func(peer *Peer) bool {
		// MsgNewAccepted was introduced in protocol 'thor/2'
		return peer.TestCap(2) && !peer.IsAcceptedKnown(acc.Hash())
	})

	for _, peer := range peers {
		peer := peer
		peer.MarkAccepted(acc.Hash())
		c.goes.Go(func() {
			if err := proto.NotifyNewAccepted(c.ctx, peer, acc); err != nil {
				peer.logger.Debug("failed to broadcast new accepted message", "err", err)
			}
		})
	}

	if local {
		c.goes.Go(func() {
			// accepted is generated locally, broadcast to notify packer loop
			c.newAcceptedFeed.Send(&NewAcceptedEvent{
				Accepted: acc,
			})
		})
	}
}

// SubscribeDraft subscribe the event that new draft received.
func (c *Communicator) SubscribeDraft(ch chan *NewDraftEvent) event.Subscription {
	return c.feedScope.Track(c.newDraftFeed.Subscribe(ch))
}

// SubscribeAccepted subscribe the event that new accepted message received.
func (c *Communicator) SubscribeAccepted(ch chan *NewAcceptedEvent) event.Subscription {
	return c.feedScope.Track(c.newAcceptedFeed.Subscribe(ch))
}
