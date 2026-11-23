// Copyright (c) 2026 The VeChainThor developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package p2psrv

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/p2p/enr"
	"github.com/stretchr/testify/require"
)

func ids(nodes []*enode.Node) []enode.ID {
	out := make([]enode.ID, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, n.ID())
	}
	return out
}

func TestParseBootnodeList(t *testing.T) {
	a, b := fakeNode(1), fakeNode(2)
	incomplete := enode.NewV4(a.Pubkey(), nil, 0, 0)
	list := strings.Join([]string{
		"# comment", "",
		"  " + a.URLv4() + "  ", "not a node", incomplete.URLv4(),
		"[legacy]", // section headers are no longer special: just an unparsable line
		b.URLv4(),
	}, "\n")
	got := parseBootnodeList(strings.NewReader(list))
	require.Equal(t, []enode.ID{a.ID(), b.ID()}, ids(got))
}

func TestParseBootnodeListAcceptsENR(t *testing.T) {
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	var r enr.Record
	r.Set(enr.IP(net.IPv4(127, 0, 0, 1)))
	r.Set(enr.UDP(30303))
	r.Set(enr.TCP(30303))
	require.NoError(t, enode.SignV4(&r, key))
	n, err := enode.New(enode.ValidSchemes, &r)
	require.NoError(t, err)

	got := parseBootnodeList(strings.NewReader(n.String() + "\n"))
	require.Equal(t, []enode.ID{n.ID()}, ids(got))
}

func TestParseBootnodeListStopsAtSizeLimit(t *testing.T) {
	filler := "#" + strings.Repeat("x", 62) + "\n" // 64 bytes per line
	list := strings.Repeat(filler, maxBootnodeListBytes/len(filler)) +
		fakeNode(1).URLv4() + "\n"
	got := parseBootnodeList(strings.NewReader(list))
	require.Empty(t, got)
}

func TestParseBootnodeListSurvivesLongLine(t *testing.T) {
	n := fakeNode(1)
	list := strings.Join([]string{
		"#" + strings.Repeat("x", 100<<10),
		strings.Repeat("y", 100<<10),
		n.URLv4(),
	}, "\n")
	got := parseBootnodeList(strings.NewReader(list))
	require.Equal(t, []enode.ID{n.ID()}, ids(got))
}

func TestFetchBootnodeList(t *testing.T) {
	n := fakeNode(1)

	t.Run("ok", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(n.URLv4() + "\n"))
		}))
		defer srv.Close()
		got, err := fetchBootnodeList(t.Context(), srv.URL)
		require.NoError(t, err)
		require.Equal(t, []enode.ID{n.ID()}, ids(got))
	})

	t.Run("non-200", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()
		_, err := fetchBootnodeList(t.Context(), srv.URL)
		require.Error(t, err)
	})

	t.Run("closed server", func(t *testing.T) {
		srv := httptest.NewServer(http.NotFoundHandler())
		url := srv.URL
		srv.Close()
		_, err := fetchBootnodeList(t.Context(), url)
		require.Error(t, err)
	})

	t.Run("deadline", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			select {
			case <-r.Context().Done():
			case <-time.After(5 * time.Second):
			}
		}))
		defer srv.Close()
		ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
		defer cancel()
		start := time.Now()
		_, err := fetchBootnodeList(ctx, srv.URL)
		require.Error(t, err)
		require.Less(t, time.Since(start), 2*time.Second)
	})
}

// signedNode builds a signed ENR record carrying the given IPv6 address and UDP port.
func signedNode(t *testing.T, ip6 net.IP) *enode.Node {
	t.Helper()
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	var r enr.Record
	r.Set(enr.IPv6(ip6))
	r.Set(enr.UDP6(30303))
	r.Set(enr.UDP(30303)) // a 4in6 address reads the udp key
	require.NoError(t, enode.SignV4(&r, key))
	n, err := enode.New(enode.ValidSchemes, &r)
	require.NoError(t, err)
	return n
}

func TestParseBootnodeListSkipsUnspecifiedAndMulticast(t *testing.T) {
	good := fakeNode(1)
	unspec := signedNode(t, net.ParseIP("::ffff:0.0.0.0"))
	// the case only means something if upstream accepts the node
	require.NoError(t, unspec.ValidateComplete())
	// upstream already drops multicast addresses, including the 4in6 form
	mcast := enode.NewV4(good.Pubkey(), net.ParseIP("224.0.0.1"), 30303, 30303)

	got := parseBootnodeList(strings.NewReader(strings.Join([]string{
		unspec.String(), mcast.String(), good.URLv4(),
	}, "\n")))
	require.Equal(t, []enode.ID{good.ID()}, ids(got))
}

func TestParseBootnodeListCRLF(t *testing.T) {
	a, b := fakeNode(1), fakeNode(2)
	lines := []string{"# comment", a.URLv4(), b.URLv4(), ""}
	got := parseBootnodeList(strings.NewReader(strings.Join(lines, "\r\n")))
	require.Equal(t, []enode.ID{a.ID(), b.ID()}, ids(got))
}

func TestParseBootnodeListManyBadLines(t *testing.T) {
	var lines []string
	for i := range 100 {
		lines = append(lines, fmt.Sprintf("<html>garbage %d</html>", i))
	}
	got := parseBootnodeList(strings.NewReader(strings.Join(lines, "\n")))
	require.Empty(t, got)
}

func TestFetchBootnodeListFailsOnBodyReadError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(fakeNode(1).URLv4() + "\n"))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	_, err := fetchBootnodeList(ctx, srv.URL)
	require.Error(t, err)
}
