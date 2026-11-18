// Copyright (c) 2026 The VeChainThor developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package p2psrv

import (
	"bytes"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fakeNode(seed byte) *enode.Node {
	// deterministic key yields a valid v4 node
	key, _ := crypto.ToECDSA(bytes.Repeat([]byte{seed}, 32))
	return enode.NewV4(&key.PublicKey, net.IPv4(10, 0, 0, seed), 30303, 30303)
}

func TestCandidateIterDedupAndDrain(t *testing.T) {
	it := newCandidateIter(8)
	n := fakeNode(1)
	it.Add(n)
	it.Add(n) // deduped by ID
	require.True(t, it.Next())
	assert.Equal(t, n.ID(), it.Node().ID())
	// buffer drained: Next must block until a new candidate arrives
	got := make(chan bool, 1)
	go func() { got <- it.Next() }()
	select {
	case <-got:
		t.Fatal("Next should block on empty buffer")
	case <-time.After(50 * time.Millisecond):
	}
	it.Add(fakeNode(2))
	assert.True(t, <-got)
	it.Close()
}

func TestCandidateIterCloseUnblocksNext(t *testing.T) {
	it := newCandidateIter(8)
	got := make(chan bool, 1)
	go func() { got <- it.Next() }()
	time.Sleep(20 * time.Millisecond)
	it.Close()
	select {
	case v := <-got:
		assert.False(t, v) // Next stays false after Close (FairMix removal semantics)
	case <-time.After(time.Second):
		t.Fatal("Close did not unblock Next")
	}
	assert.False(t, it.Next())
	it.Close() // idempotent, must not panic
}

func TestCandidateIterEviction(t *testing.T) {
	it := newCandidateIter(4)
	for i := byte(1); i <= 8; i++ {
		it.Add(fakeNode(i))
	}
	// capacity 4: evicts one at a time when full, so exactly 4 remain
	count := 0
	for it.tryPick() != nil { // non-blocking pick
		count++
	}
	assert.Equal(t, 4, count)
	it.Close()
}

func TestCandidateIterConcurrent(t *testing.T) {
	it := newCandidateIter(16)
	var wg sync.WaitGroup
	wg.Go(func() {
		for i := byte(1); i < 100; i++ {
			it.Add(fakeNode(i%16 + 1))
		}
		it.Close()
	})
	for it.Next() {
	}
	wg.Wait()
}
