// Copyright (c) 2026 The VeChainThor developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package p2psrv

import (
	"encoding/binary"
	"net"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/p2p/rlpx"
	"github.com/stretchr/testify/require"
)

// Upstream has no test for the 2KB cap on the pre-auth handshake packet; pin it here.
func TestRLPxHandshakeSizeLimit(t *testing.T) {
	p1, p2 := net.Pipe()
	defer p1.Close()
	defer p2.Close()
	go func() {
		// EIP-8 prefix: 2-byte big-endian size claiming 2049, followed by 2049 bytes of junk
		var pre [2]byte
		binary.BigEndian.PutUint16(pre[:], 2049)
		p2.Write(pre[:])
		p2.Write(make([]byte, 2049))
	}()
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	conn := rlpx.NewConn(p1, nil) // recipient
	_, err = conn.Handshake(key)
	require.ErrorContains(t, err, "message too big")
}
