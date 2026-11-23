// Copyright (c) 2026 The VeChainThor developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package p2psrv

import (
	"net/http"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func noLegacy(o *Options) { o.NoLegacyDiscovery = true }

func TestNoLegacyDiscoveryStartsV5Only(t *testing.T) {
	s := newListServer(t, "", noLegacy)
	require.NoError(t, s.Start(testApp{topic: "list"}))
	assert.Nil(t, s.legacy)
	assert.NotNil(t, s.v5)
	stopWithinTimeout(t, s)
}

func TestNoLegacyDiscoveryStillDiscoversPeers(t *testing.T) {
	boot := startSmoke(t, 10, false, withOpts(noLegacy))
	b := startSmoke(t, 10, false, withOpts(noLegacy), withV5Bootnodes(boot.LocalNode().Node()))
	c := startSmoke(t, 10, false, withOpts(noLegacy), withV5Bootnodes(boot.LocalNode().Node()))

	require.Eventually(t, func() bool {
		return b.peerCount() > 0 && c.peerCount() > 0 && hasPeer(b, c) && hasPeer(c, b)
	}, smokeWait, smokeTick)

	for _, n := range []*smokeNode{boot, b, c} {
		n.stopWithin(t, smokeStopTimeout)
	}
}

func hasPeer(n, other *smokeNode) bool {
	for _, p := range n.srv.Peers() {
		if p.ID() == other.Self().ID() {
			return true
		}
	}
	return false
}

func TestNoLegacyDiscoveryStillDialsKnownNodes(t *testing.T) {
	a := startSmoke(t, 10, false)
	b := startSmoke(t, 10, false, withOpts(func(o *Options) {
		o.NoLegacyDiscovery = true
		o.KnownNodes = append(o.KnownNodes, a.Self())
	}))
	require.Nil(t, b.legacy)
	require.Eventually(t, func() bool { return hasPeer(b, a) }, smokeWait, smokeTick)
}

func TestNoLegacyDiscoverySkipsRetry(t *testing.T) {
	setRemoteListTimings(t, 2*time.Second, 20*time.Millisecond)
	url, count := serveList(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	s := newListServer(t, url, noLegacy)
	startListServer(t, s)

	time.Sleep(200 * time.Millisecond)
	assert.EqualValues(t, 1, count.Load())
}

func TestNoLegacyDiscoverySeedsV5FromRemoteList(t *testing.T) {
	setRemoteListTimings(t, 2*time.Second, time.Hour)
	n := loopNode(1)
	url, count := serveList(t, listBody(n.URLv4()))
	s := newListServer(t, url, noLegacy)
	startListServer(t, s)

	assert.Equal(t, []enode.ID{n.ID()}, ids(s.v5Bootnodes))
	assert.Empty(t, s.bootstrapNodes)
	assert.EqualValues(t, 1, count.Load())
}
