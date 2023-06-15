// Copyright (c) 2023 The VeChainThor developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>
package subscriptions

import (
	"sync"

	"github.com/vechain/thor/tx"
	"github.com/vechain/thor/txpool"
)

type pendingTx struct {
	txPool    *txpool.TxPool
	listeners map[*listener]struct{}
	mu        sync.RWMutex
	done      chan struct{}
}

func newPendingTx(txPool *txpool.TxPool) *pendingTx {
	pt := &pendingTx{
		txPool:    txPool,
		listeners: make(map[*listener]struct{}),
		done:      make(chan struct{}),
	}

	return pt
}

func (p *pendingTx) Subscribe() *listener {
	p.mu.Lock()
	defer p.mu.Unlock()

	lsn := &listener{
		ch:  make(chan *tx.Transaction),
		ptx: p,
	}
	p.listeners[lsn] = struct{}{}
	return lsn
}

func (p *pendingTx) Unsubscribe(lsn *listener) {
	p.mu.Lock()
	defer p.mu.Unlock()

	lsn.Close()
}

func (p *pendingTx) Start() {
	txCh := make(chan *txpool.TxEvent)
	sub := p.txPool.SubscribeTxEvent(txCh)

	defer func() {
		sub.Unsubscribe()

		p.mu.Lock()
		for lsn := range p.listeners {
			lsn.Close()
		}
		p.mu.Unlock()
	}()

	for {
		select {
		case txEv := <-txCh:
			p.mu.RLock()
			for lsn := range p.listeners {
				select {
				case lsn.ch <- txEv.Tx:
				case <-p.done:
					return
				default: // broadcast in a non-blocking manner, so there's no guarantee that all subscriber receives it
				}
			}
			p.mu.RUnlock()
		case <-p.done:
			return
		}
	}
}

func (p *pendingTx) Stop() {
	close(p.done)
}

type listener struct {
	ch   chan *tx.Transaction
	ptx  *pendingTx
	once sync.Once
}

func (l *listener) Close() {
	l.once.Do(func() {
		close(l.ch)
		delete(l.ptx.listeners, l)
	})
}
