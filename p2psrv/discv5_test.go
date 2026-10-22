// Copyright (c) 2026 The VeChainThor developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package p2psrv

import (
	"net"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/p2p/discover"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/p2p/enr"
	"github.com/stretchr/testify/require"
)

func listenTestV5(t *testing.T, id *[6]byte) *discover.UDPv5 {
	t.Helper()
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)
	db, err := enode.OpenDB("")
	require.NoError(t, err)
	ln := enode.NewLocalNode(db, key)
	ln.SetFallbackIP(net.IPv4(127, 0, 0, 1))
	ln.SetFallbackUDP(conn.LocalAddr().(*net.UDPAddr).Port)
	v5, err := discover.ListenV5(conn, ln, discover.Config{PrivateKey: key, V5ProtocolID: id})
	require.NoError(t, err)
	t.Cleanup(func() { v5.Close(); db.Close() })
	return v5
}

func TestV5ProtocolIDIsolation(t *testing.T) {
	a := listenTestV5(t, &V5ProtocolID)
	b := listenTestV5(t, &V5ProtocolID)
	_, err := a.Ping(b.Self())
	require.NoError(t, err, "same protocol id must talk")

	eth := listenTestV5(t, nil) // upstream default "discv5"
	_, err = a.Ping(eth.Self())
	require.Error(t, err, "vechain node must get no answer from a default-id node")
	_, err = eth.Ping(a.Self())
	require.Error(t, err, "default-id node must get no answer from a vechain node")
}

func TestLocalNodeAdvertisesLegacyUDPPort(t *testing.T) {
	n := startSmoke(t, 1, false)
	want := int(n.discv5.Self().UDP) // the socket legacy discv5 owns
	got := n.LocalNode().Node().UDP()
	require.Equal(t, want, got, "ENR must carry the shared discovery port, not the parked upstream socket")
	require.NotZero(t, got)
}

func TestToV5NodeFillsTCPFromUDP(t *testing.T) {
	key, _ := crypto.GenerateKey()
	var r enr.Record
	r.Set(enr.IPv4(net.IPv4(10, 0, 0, 9)))
	r.Set(enr.UDP(55555))
	require.NoError(t, enode.SignV4(&r, key))
	n, _ := enode.New(enode.ValidSchemes, &r)
	legacy := toV5Node(n)
	require.EqualValues(t, 55555, legacy.UDP)
	require.EqualValues(t, 55555, legacy.TCP, "shared-port bootnode: legacy record mirrors disco's own tcp=udp endpoint")
	require.EqualValues(t, 30303, toV5Node(fakeNode(1)).TCP, "explicit tcp is kept")

	// the synthesized tcp is what lets legacy SetFallbackNodes accept the record
	key2, _ := crypto.GenerateKey()
	s := New(&Options{Name: "x", PrivateKey: key2, MaxPeers: 1, ListenAddr: "127.0.0.1:0", DiscoveryNodes: []*enode.Node{n}})
	require.NoError(t, s.Start(nil, "smoke"))
	s.Stop()
}

func TestStartRejectsBadV5Bootnode(t *testing.T) {
	key, _ := crypto.GenerateKey()
	noUDP := enode.NewV4(&key.PublicKey, net.IPv4(10, 0, 0, 1), 30303, 0)
	s := New(&Options{Name: "x", PrivateKey: key, MaxPeers: 1, ListenAddr: "127.0.0.1:0", DiscoveryNodesV5: []*enode.Node{noUDP}})
	err := s.Start(nil, "smoke")
	require.ErrorContains(t, err, "listen discv5")
	s.Stop() // must not hang or panic after a failed start
}
