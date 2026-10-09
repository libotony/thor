// Copyright (c) 2026 The VeChainThor developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package p2psrv

import (
	"sync"

	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/vechain/thor/v2/cache"
)

// candidateIter adapts discovered nodes into an enode.Iterator.
// Backed by a RandCache: dedup by node ID, random eviction when full
// (keeps content fresh under slot starvation). Add never blocks the
// producer (discv5 main loop liveness); Close unblocks a pending
// Next and makes it return false forever (FairMix removal semantics).
type candidateIter struct {
	cache     *cache.RandCache
	notify    chan struct{}
	closed    chan struct{}
	closeOnce sync.Once
	cur       *enode.Node
}

var _ enode.Iterator = (*candidateIter)(nil)

func newCandidateIter(size int) *candidateIter {
	return &candidateIter{
		cache:  cache.NewRandCache(size),
		notify: make(chan struct{}, 1),
		closed: make(chan struct{}),
	}
}

func (it *candidateIter) Add(n *enode.Node) {
	it.cache.Set(n.ID(), n)
	select {
	case it.notify <- struct{}{}:
	default:
	}
}

func (it *candidateIter) tryPick() *enode.Node {
	entry := it.cache.Pick()
	if entry == nil {
		return nil
	}
	it.cache.Remove(entry.Key)
	return entry.Value.(*enode.Node)
}

func (it *candidateIter) Next() bool {
	for {
		select {
		case <-it.closed:
			return false
		default:
		}
		if n := it.tryPick(); n != nil {
			it.cur = n
			return true
		}
		select {
		case <-it.closed:
			return false
		case <-it.notify:
		}
	}
}

func (it *candidateIter) Node() *enode.Node { return it.cur }

func (it *candidateIter) Close() {
	it.closeOnce.Do(func() { close(it.closed) })
}
