// Copyright (c) 2026 The VeChainThor developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package comm

import (
	"bytes"
	"crypto/ecdsa"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/p2p"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vechain/thor/v2/comm/proto"
	"github.com/vechain/thor/v2/forkid"
	"github.com/vechain/thor/v2/p2p/discv5"
	"github.com/vechain/thor/v2/test/testchain"
	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/txpool"
)

// statusRW answers every outgoing call with the canned reply as call result.
type statusRW struct {
	read  chan p2p.Msg
	reply any
}

func (rw *statusRW) ReadMsg() (p2p.Msg, error) {
	msg, ok := <-rw.read
	if !ok {
		return p2p.Msg{}, io.EOF
	}
	return msg, nil
}

func (rw *statusRW) WriteMsg(msg p2p.Msg) error {
	s := rlp.NewStream(msg.Payload, uint64(msg.Size))
	if _, err := s.List(); err != nil {
		return err
	}
	var callID uint32
	if err := s.Decode(&callID); err != nil {
		return err
	}
	enc, err := rlp.EncodeToBytes([]any{callID, true, rw.reply})
	if err != nil {
		return err
	}
	rw.read <- p2p.Msg{Code: msg.Code, Size: uint32(len(enc)), Payload: bytes.NewReader(enc)}
	return nil
}

// servedPeer returns a peer whose remote end answers every call with reply.
func servedPeer(t *testing.T, version uint, reply any) *Peer {
	t.Helper()
	rw := &statusRW{read: make(chan p2p.Msg, 1), reply: reply}
	peer := newPeer(p2p.NewPeer(enode.ID{}, "remote", nil), rw, version)
	go peer.Serve(func(*p2p.Msg, func(any)) error { return nil }, proto.MaxMsgSize)
	t.Cleanup(func() { close(rw.read) })
	return peer
}

// newHandshakeChain builds a SoloFork chain whose head is past the
// INTERSTELLAR height (1), i.e. the local fork id is {checksum, Next: 0}.
func newHandshakeChain(t *testing.T) (*testchain.Chain, *txpool.TxPool) {
	t.Helper()
	chain, err := testchain.NewWithFork(&thor.SoloFork, 180)
	require.NoError(t, err)
	require.NoError(t, chain.MintBlocks(2))

	pool := txpool.New(chain.Repo(), chain.Stater(), txpool.Options{Limit: 10, LimitPerAccount: 2, MaxLifetime: time.Minute}, &thor.SoloFork)
	t.Cleanup(pool.Close)
	return chain, pool
}

// The topic literal is deliberately not built from proto.Name/proto.Version:
// the test pins the exact bytes nodes of every protocol version advertise.
func TestDiscTopic(t *testing.T) {
	chain, pool := newHandshakeChain(t)
	c := New(chain.Repo(), pool, &thor.SoloFork)
	t.Cleanup(c.cancel)

	genesisID := chain.Repo().GenesisBlock().Header().ID()
	assert.Equal(t, discv5.Topic(fmt.Sprintf("thor1@%x", genesisID[24:])), c.DiscTopic())
}

func TestGetStatusBothVersions(t *testing.T) {
	var (
		genesisID = thor.Bytes32{0x01}
		bestID    = thor.Bytes32{0x03}
		forkID    = forkid.ID{Hash: [4]byte{0xaa, 0xbb, 0xcc, 0xdd}, Next: 7}
	)

	t.Run("thor/1 status is lifted with zero fork id", func(t *testing.T) {
		// wire order of the frozen thor/1 status: genesis, timestamp, best, score
		peer := servedPeer(t, proto.V1, []any{genesisID, uint64(2), bestID, uint64(4)})

		got, err := proto.GetStatus(t.Context(), peer, proto.V1)
		require.NoError(t, err)
		assert.Equal(t, genesisID, got.GenesisBlockID)
		assert.Equal(t, uint64(2), got.SysTimestamp)
		assert.Equal(t, bestID, got.BestBlockID)
		assert.Equal(t, uint64(4), got.TotalScore)
		assert.Equal(t, forkid.ID{}, got.ForkID)
	})

	t.Run("thor/2 status is decoded as is", func(t *testing.T) {
		peer := servedPeer(t, proto.Version, []any{genesisID, uint64(2), bestID, uint64(4), forkID})

		got, err := proto.GetStatus(t.Context(), peer, proto.Version)
		require.NoError(t, err)
		assert.Equal(t, &proto.Status{
			GenesisBlockID: genesisID,
			SysTimestamp:   2,
			BestBlockID:    bestID,
			TotalScore:     4,
			ForkID:         forkID,
		}, got)
	})

	t.Run("thor/2 call rejects a thor/1 status", func(t *testing.T) {
		peer := servedPeer(t, proto.Version, []any{genesisID, uint64(2), bestID, uint64(4)})

		_, err := proto.GetStatus(t.Context(), peer, proto.Version)
		assert.Error(t, err)
	})
}

func TestRunPeerForkIDMatrix(t *testing.T) {
	chain, pool := newHandshakeChain(t)
	genesisID := chain.Repo().GenesisBlock().Header().ID()
	best := chain.Repo().BestBlockSummary().Header
	localID := forkid.NewID(genesisID, &thor.SoloFork, best.Number())
	require.Zero(t, localID.Next, "precondition: local head passed the only scheduled fork")

	// remote never scheduled the fork the local chain has passed
	staleID := forkid.NewID(genesisID, &thor.NoFork, 0)
	require.NotEqual(t, localID.Hash, staleID.Hash)

	v2Status := func(genesis thor.Bytes32, id forkid.ID) *proto.Status {
		return &proto.Status{
			GenesisBlockID: genesis,
			SysTimestamp:   uint64(time.Now().Unix()),
			BestBlockID:    best.ID(),
			TotalScore:     best.TotalScore(),
			ForkID:         id,
		}
	}

	tests := []struct {
		name     string
		version  uint
		reply    any
		accepted bool
	}{
		{
			// thor/1 carries no fork id; the zero ForkID must not reach the filter
			name:    "thor/1 with matching genesis",
			version: proto.V1,
			reply: &proto.StatusV1{
				GenesisBlockID: genesisID,
				SysTimestamp:   uint64(time.Now().Unix()),
				BestBlockID:    best.ID(),
				TotalScore:     best.TotalScore(),
			},
			accepted: true,
		},
		{"thor/2 with matching fork id", proto.Version, v2Status(genesisID, localID), true},
		{"thor/2 with foreign chain fork id", proto.Version, v2Status(genesisID, forkid.ID{Hash: [4]byte{0xde, 0xad, 0xbe, 0xef}}), false},
		{"thor/2 with stale fork id", proto.Version, v2Status(genesisID, staleID), false},
		{"thor/2 with mismatching genesis", proto.Version, v2Status(thor.Bytes32{0xff}, localID), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			peer := servedPeer(t, tt.version, tt.reply)
			c := New(chain.Repo(), pool, &thor.SoloFork)
			t.Cleanup(c.cancel)

			done := make(chan struct{})
			go func() {
				defer close(done)
				c.runPeer(peer)
			}()

			if tt.accepted {
				require.Eventually(t, func() bool { return c.peerSet.Len() == 1 }, 3*time.Second, 5*time.Millisecond)
				select {
				case <-done:
					t.Fatal("runPeer returned although the peer was accepted")
				default:
				}
			} else {
				select {
				case <-done:
				case <-time.After(3 * time.Second):
					t.Fatal("runPeer did not reject the peer")
				}
				assert.Zero(t, c.peerSet.Len())
			}

			// an accepted peer is removed once runPeer unblocks on ctx cancel
			c.cancel()
			<-done
			assert.Zero(t, c.peerSet.Len())
		})
	}
}

// bufPipe is a buffered in-memory MsgReadWriter. p2p.MsgPipe is unusable for
// two-ended handshakes: its payload is not a *bytes.Reader (rpc.Serve reads
// the header through a bufio-wrapped stream, leaving nothing for msg.Decode),
// and it is unbuffered while rpc.Serve writes replies inside its read loop,
// so two peers calling GetStatus at once deadlock.
type bufPipe struct {
	r      <-chan p2p.Msg
	w      chan<- p2p.Msg
	closed chan struct{}
}

func newBufPipe() (*bufPipe, *bufPipe) {
	a, b := make(chan p2p.Msg, 64), make(chan p2p.Msg, 64)
	closed := make(chan struct{})
	return &bufPipe{a, b, closed}, &bufPipe{b, a, closed}
}

func (p *bufPipe) ReadMsg() (p2p.Msg, error) {
	select {
	case m := <-p.r:
		return m, nil
	case <-p.closed:
		return p2p.Msg{}, io.EOF
	}
}

func (p *bufPipe) WriteMsg(m p2p.Msg) error {
	data, err := io.ReadAll(m.Payload)
	if err != nil {
		return err
	}
	m.Payload = bytes.NewReader(data)
	select {
	case p.w <- m:
		return nil
	case <-p.closed:
		return io.EOF
	}
}

// TestProtocolsHandshake runs two Communicators against each other through
// Protocols(), pinning that each registered entry carries its own version into
// the handshake: thor/1 skips the fork id filter, thor/2 applies it.
func TestProtocolsHandshake(t *testing.T) {
	chain, pool := newHandshakeChain(t)

	// head is 2, past the SoloFork INTERSTELLAR height; NoFork never scheduled it
	passed, unscheduled := &thor.SoloFork, &thor.NoFork

	requireAccepted := func(t *testing.T, version uint, cs ...*Communicator) {
		t.Helper()
		for _, c := range cs {
			require.Eventually(t, func() bool { return c.peerSet.Len() == 1 }, 3*time.Second, 5*time.Millisecond)
			assert.Equal(t, version, c.peerSet.Slice()[0].Version())
		}
	}

	handshake := func(t *testing.T, entry int, a, b *thor.ForkConfig) (*Communicator, *Communicator) {
		t.Helper()
		c1, c2 := New(chain.Repo(), pool, a), New(chain.Repo(), pool, b)
		t.Cleanup(c1.cancel)
		t.Cleanup(c2.cancel)

		rw1, rw2 := newBufPipe()
		t.Cleanup(func() { close(rw1.closed) })
		go c1.Protocols()[entry].Run(p2p.NewPeer(enode.ID{2}, "c2", nil), rw1)
		go c2.Protocols()[entry].Run(p2p.NewPeer(enode.ID{1}, "c1", nil), rw2)
		return c1, c2
	}

	t.Run("thor/2 compatible fork config is accepted", func(t *testing.T) {
		c1, c2 := handshake(t, 1, passed, passed)
		requireAccepted(t, proto.Version, c1, c2)
	})

	t.Run("thor/1 skips the filter for incompatible fork config", func(t *testing.T) {
		c1, c2 := handshake(t, 0, passed, unscheduled)
		requireAccepted(t, proto.V1, c1, c2)
	})

	t.Run("thor/2 rejects incompatible fork config", func(t *testing.T) {
		c1, c2 := handshake(t, 1, passed, unscheduled)
		// negative: bounded wait, cannot detect rejection deterministically
		require.Never(t, func() bool { return c1.peerSet.Len() > 0 || c2.peerSet.Len() > 0 }, 500*time.Millisecond, 10*time.Millisecond)
	})
}

// p2p.NewPeer cannot carry a pubkey, so the public peer ID format is pinned over a real
// handshake: inbound and outbound peers must both render the remote's 128-hex pubkey,
// and the advertised name must survive untruncated (p2p.Peer.Name cuts at 20 chars).
func TestLegacyPeerIDFormat(t *testing.T) {
	const longName = "name-longer-than-twenty-characters"

	newServer := func(name string, peers chan<- *p2p.Peer) (*p2p.Server, *ecdsa.PrivateKey) {
		key, err := crypto.GenerateKey()
		require.NoError(t, err)
		srv := &p2p.Server{Config: p2p.Config{
			PrivateKey:  key,
			Name:        name,
			MaxPeers:    2,
			NoDiscovery: true,
			ListenAddr:  "127.0.0.1:0",
			Protocols: []p2p.Protocol{{
				Name:    "legacyid",
				Version: 1,
				Length:  1,
				Run: func(peer *p2p.Peer, rw p2p.MsgReadWriter) error {
					peers <- peer
					for {
						if _, err := rw.ReadMsg(); err != nil {
							return err
						}
					}
				},
			}},
		}}
		require.NoError(t, srv.Start())
		t.Cleanup(srv.Stop)
		return srv, key
	}
	pubHex := func(key *ecdsa.PrivateKey) string {
		return fmt.Sprintf("%x", crypto.FromECDSAPub(&key.PublicKey)[1:])
	}

	dialerPeers, listenerPeers := make(chan *p2p.Peer, 1), make(chan *p2p.Peer, 1)
	dialer, dialerKey := newServer("dialer", dialerPeers)
	listener, listenerKey := newServer(longName, listenerPeers)

	conn, err := net.Dial("tcp", listener.ListenAddr)
	require.NoError(t, err)
	require.NoError(t, dialer.SetupConn(conn, 1, listener.Self()))

	outbound, inbound := <-dialerPeers, <-listenerPeers
	require.False(t, outbound.Inbound())
	require.True(t, inbound.Inbound())

	assert.Len(t, legacyPeerID(outbound), 128)
	assert.Equal(t, pubHex(listenerKey), legacyPeerID(outbound))
	assert.Equal(t, pubHex(dialerKey), legacyPeerID(inbound))
	assert.Equal(t, longName, outbound.Fullname())
	assert.NotEqual(t, longName, outbound.Name())

	// the API entry point reads the same two fields
	chain, pool := newHandshakeChain(t)
	c := New(chain.Repo(), pool, &thor.SoloFork)
	t.Cleanup(c.cancel)
	c.peerSet.Add(newPeer(outbound, stubMsgReadWriter{}, proto.Version))
	stats := c.PeersStats()
	require.Len(t, stats, 1)
	assert.Equal(t, pubHex(listenerKey), stats[0].PeerID)
	assert.Equal(t, longName, stats[0].Name)
}
