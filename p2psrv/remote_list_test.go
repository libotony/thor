// Copyright (c) 2026 The VeChainThor developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package p2psrv

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vechain/thor/v2/p2p/discv5"
)

// loopNode is a complete node on a loopback address nobody listens on.
func loopNode(seed byte) *enode.Node {
	key, _ := crypto.ToECDSA(bytes.Repeat([]byte{seed}, 32))
	return enode.NewV4(&key.PublicKey, net.IPv4(127, 0, 0, 1), 1, 1)
}

func setRemoteListTimings(t *testing.T, timeout, retry time.Duration) {
	oldTimeout, oldRetry := remoteListTimeout, remoteListRetryInterval
	remoteListTimeout, remoteListRetryInterval = timeout, retry
	t.Cleanup(func() { remoteListTimeout, remoteListRetryInterval = oldTimeout, oldRetry })
}

// serveList starts an httptest server and returns its URL and a request counter.
func serveList(t *testing.T, handler http.HandlerFunc) (string, *atomic.Int32) {
	var count atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		handler(w, r)
	}))
	t.Cleanup(ts.Close)
	return ts.URL, &count
}

func listBody(lines ...string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, strings.Join(lines, "\n"))
	}
}

func newListServer(t *testing.T, url string, mods ...func(*Options)) *Server {
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	opts := &Options{
		Name:                "list",
		PrivateKey:          key,
		MaxPeers:            2,
		ListenAddr:          "127.0.0.1:0",
		DiscoveryNodes:      []*enode.Node{},
		RemoteDiscoveryList: url,
	}
	for _, mod := range mods {
		mod(opts)
	}
	return New(opts)
}

func startListServer(t *testing.T, s *Server) {
	require.NoError(t, s.Start(testApp{topic: "list"}))
	t.Cleanup(s.Stop)
}

func legacyIDs(nodes []*discv5.Node) []discv5.NodeID {
	out := make([]discv5.NodeID, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, n.ID)
	}
	return out
}

func TestStartSeedsBothStacksFromRemoteList(t *testing.T) {
	setRemoteListTimings(t, 2*time.Second, time.Hour)
	a, b := loopNode(1), loopNode(2)
	url, count := serveList(t, listBody(a.URLv4(), b.URLv4()))
	s := newListServer(t, url)
	startListServer(t, s)

	legacy := legacyIDs(s.bootstrapNodes)
	assert.Equal(t, []discv5.NodeID{toLegacyNode(a).ID, toLegacyNode(b).ID}, legacy)
	assert.Equal(t, []enode.ID{a.ID(), b.ID()}, ids(s.v5Bootnodes))
	assert.EqualValues(t, 1, count.Load())
}

func TestStartContinuesWhenRemoteListFails(t *testing.T) {
	const timeout = 100 * time.Millisecond
	setRemoteListTimings(t, timeout, time.Hour)

	release := make(chan struct{})
	closed := httptest.NewServer(nil)
	closed.Close()

	cases := map[string]string{}
	cases["timeout"], _ = serveList(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	})
	t.Cleanup(func() { close(release) }) // runs before the httptest server closes
	cases["500"], _ = serveList(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	cases["closed"] = closed.URL

	for name, url := range cases {
		t.Run(name, func(t *testing.T) {
			explicit := loopNode(9)
			s := newListServer(t, url, func(o *Options) {
				o.DiscoveryNodesV5 = []*enode.Node{explicit}
			})
			begin := time.Now()
			require.NoError(t, s.Start(testApp{topic: "list"}))
			t.Cleanup(s.Stop)
			assert.Less(t, time.Since(begin), timeout+2*time.Second)
			assert.Equal(t, s.opts.DiscoveryNodesV5, s.v5Bootnodes)
		})
	}
}

func TestStartFetchesListBeforeListening(t *testing.T) {
	setRemoteListTimings(t, 2*time.Second, time.Hour)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	require.NoError(t, ln.Close())

	var dialErr atomic.Value
	url, count := serveList(t, func(w http.ResponseWriter, _ *http.Request) {
		conn, err := net.DialTimeout("tcp", addr, time.Second)
		if err == nil {
			conn.Close()
		}
		dialErr.Store(fmt.Sprint(err))
		listBody()(w, nil)
	})
	s := newListServer(t, url, func(o *Options) { o.ListenAddr = addr })
	startListServer(t, s)

	require.EqualValues(t, 1, count.Load())
	assert.NotEqual(t, "<nil>", dialErr.Load(), "dial inside the handler must fail: not listening yet")
	conn, err := net.DialTimeout("tcp", addr, time.Second)
	require.NoError(t, err)
	conn.Close()
}

func TestStartSkipsRemoteListWhenNotWanted(t *testing.T) {
	setRemoteListTimings(t, 2*time.Second, time.Hour)
	url, count := serveList(t, listBody())

	startListServer(t, newListServer(t, ""))
	startListServer(t, newListServer(t, url, func(o *Options) { o.NoDiscovery = true }))
	assert.Zero(t, count.Load())
}

func TestRemoteListRetryUpdatesLegacyOnly(t *testing.T) {
	setRemoteListTimings(t, 2*time.Second, 20*time.Millisecond)
	l := loopNode(1)
	good := listBody(l.URLv4())
	var calls atomic.Int32
	url, count := serveList(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		good(w, r)
	})
	s := newListServer(t, url)
	startListServer(t, s)

	require.Eventually(t, func() bool {
		return slicesContainID(s.remoteLegacyNodes(), l.ID())
	}, 5*time.Second, 10*time.Millisecond)
	assert.Empty(t, s.v5Bootnodes)

	n := count.Load()
	time.Sleep(200 * time.Millisecond)
	assert.Equal(t, n, count.Load(), "retry must stop after the first success")
}

func slicesContainID(nodes []*enode.Node, id enode.ID) bool {
	for _, n := range nodes {
		if n.ID() == id {
			return true
		}
	}
	return false
}

func stopWithinTimeout(t *testing.T, s *Server) {
	t.Helper()
	done := make(chan struct{})
	go func() { s.Stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop did not return in time")
	}
}

func TestStopInterruptsRemoteListRetry(t *testing.T) {
	t.Run("in flight", func(t *testing.T) {
		// Long timeout: only Stop can end the blocked retry request.
		setRemoteListTimings(t, 10*time.Second, 20*time.Millisecond)
		release := make(chan struct{})
		var calls, blocked atomic.Int32
		url, _ := serveList(t, func(w http.ResponseWriter, r *http.Request) {
			if calls.Add(1) == 1 {
				http.Error(w, "boom", http.StatusInternalServerError)
				return
			}
			blocked.Add(1)
			select {
			case <-release:
			case <-r.Context().Done():
			}
		})
		t.Cleanup(func() { close(release) })

		s := newListServer(t, url)
		require.NoError(t, s.Start(testApp{topic: "list"}))
		require.Eventually(t, func() bool { return blocked.Load() >= 1 }, 5*time.Second, 10*time.Millisecond)
		stopWithinTimeout(t, s)
	})

	t.Run("between attempts", func(t *testing.T) {
		setRemoteListTimings(t, 2*time.Second, time.Hour)
		url, count := serveList(t, func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "boom", http.StatusInternalServerError)
		})
		s := newListServer(t, url)
		require.NoError(t, s.Start(testApp{topic: "list"}))
		require.EqualValues(t, 1, count.Load())
		stopWithinTimeout(t, s)
	})
}

func TestRemoteListRetryAttemptsAreBounded(t *testing.T) {
	setRemoteListTimings(t, 50*time.Millisecond, 20*time.Millisecond)
	release := make(chan struct{})
	url, count := serveList(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	})
	t.Cleanup(func() { close(release) })

	s := newListServer(t, url)
	require.NoError(t, s.Start(testApp{topic: "list"}))
	require.Eventually(t, func() bool { return count.Load() >= 4 }, 5*time.Second, 10*time.Millisecond)
	stopWithinTimeout(t, s)
}

func TestStartSurvivesBadRemoteEntries(t *testing.T) {
	setRemoteListTimings(t, 2*time.Second, time.Hour)
	good := loopNode(2)
	bad := enode.NewV4(loopNode(3).Pubkey(), nil, 0, 0)
	url, _ := serveList(t, listBody(bad.URLv4(), good.URLv4()))
	s := newListServer(t, url)
	startListServer(t, s)

	assert.Equal(t, []enode.ID{good.ID()}, ids(s.v5Bootnodes))
	assert.Equal(t, []discv5.NodeID{toLegacyNode(good).ID}, legacyIDs(s.bootstrapNodes))
}

func TestStartAcceptsDuplicateSeeds(t *testing.T) {
	setRemoteListTimings(t, 2*time.Second, time.Hour)
	n := loopNode(4)
	url, _ := serveList(t, listBody(n.URLv4()))
	s := newListServer(t, url, func(o *Options) {
		o.DiscoveryNodes = []*enode.Node{n}
		o.DiscoveryNodesV5 = []*enode.Node{n}
	})
	startListServer(t, s)
}

type countingTransport struct{ n atomic.Int32 }

func (c *countingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	c.n.Add(1)
	return nil, errors.New("no requests expected")
}

func TestStartWithoutAnySeeds(t *testing.T) {
	var requests countingTransport
	old := http.DefaultClient.Transport
	http.DefaultClient.Transport = &requests
	t.Cleanup(func() { http.DefaultClient.Transport = old })

	for name, mod := range map[string]func(*Options){
		"legacy and v5": func(*Options) {},
	} {
		t.Run(name, func(t *testing.T) {
			s := newListServer(t, "", mod)
			startListServer(t, s)
			assert.NotNil(t, s.v5)
			assert.Empty(t, s.v5Bootnodes)
			assert.Empty(t, s.bootstrapNodes)
		})
	}
	assert.Zero(t, requests.n.Load(), "no URL is configured, so no request may be made")
}

func TestSetLegacyFallbackDropsRejectedRemoteNodes(t *testing.T) {
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	network, err := discv5.ListenUDP(key, conn, conn.LocalAddr().(*net.UDPAddr), "", nil, nil)
	require.NoError(t, err)
	t.Cleanup(network.Close)

	base := []*discv5.Node{toLegacyNode(loopNode(1))}
	good := loopNode(2)
	// 4in6 unspecified address: passes upstream ValidateComplete but not the legacy stack
	bad := signedNode(t, net.ParseIP("::ffff:0.0.0.0"))
	require.NoError(t, bad.ValidateComplete())

	applied, err := setLegacyFallback(network, base, []*enode.Node{good, bad})
	require.NoError(t, err)
	assert.Equal(t, base, applied)

	applied, err = setLegacyFallback(network, base, []*enode.Node{good})
	require.NoError(t, err)
	assert.Len(t, applied, 2)
}
