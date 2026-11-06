// Copyright (c) 2025 The VeChainThor developers
//
// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package main

import (
	"encoding/hex"
	"math"
	"net"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/p2p/enr"
	"github.com/stretchr/testify/require"

	"github.com/vechain/thor/v2/chain"
	"github.com/vechain/thor/v2/cmd/thor/node"
	"github.com/vechain/thor/v2/genesis"
	"github.com/vechain/thor/v2/muxdb"
	"github.com/vechain/thor/v2/state"
	"github.com/vechain/thor/v2/thor"
)

func TestPrintStartupMessage1(t *testing.T) {
	db := muxdb.NewMem()
	stater := state.NewStater(db)
	gene, forkConfig := genesis.NewDevnet()
	b, _, _, _ := gene.Build(stater)
	repo, _ := chain.NewRepository(db, b)

	key, _ := crypto.GenerateKey()
	master := &node.Master{PrivateKey: key}

	t.Run("with master", func(t *testing.T) {
		printStartupMessage1(gene, repo, master, "/tmp/test", forkConfig)
	})

	t.Run("with beneficiary", func(t *testing.T) {
		beneficiary := thor.BytesToAddress([]byte("beneficiary"))
		master.Beneficiary = &beneficiary
		printStartupMessage1(gene, repo, master, "/tmp/test", forkConfig)
	})

	t.Run("solo mode", func(t *testing.T) {
		printStartupMessage1(gene, repo, nil, "/tmp/test", forkConfig)
	})
}

func TestPrintStartupMessage2(t *testing.T) {
	gene, _ := genesis.NewDevnet()

	t.Run("all fields", func(t *testing.T) {
		printStartupMessage2(gene, "http://localhost:8669", "enode://abc@127.0.0.1:11235", "http://localhost:2112", "http://localhost:2113", false)
	})

	t.Run("minimal fields", func(t *testing.T) {
		printStartupMessage2(gene, "http://localhost:8669", "", "", "", false)
	})

	t.Run("devnet", func(t *testing.T) {
		printStartupMessage2(gene, "http://localhost:8669", "", "", "", true)
	})
}

func TestReadIntFromUInt64Flag_WithinRange(t *testing.T) {
	got, err := readIntFromUInt64Flag(42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != 42 {
		t.Fatalf("want 42, got %d", got)
	}
}

func TestReadIntFromUInt64Flag_MaxInt(t *testing.T) {
	val := uint64(math.MaxInt)
	got, err := readIntFromUInt64Flag(val)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != int(val) {
		t.Fatalf("want %d, got %d", val, got)
	}
}

func TestReadIntFromUInt64Flag_TooLarge(t *testing.T) {
	val := uint64(math.MaxInt) + 1
	if _, err := readIntFromUInt64Flag(val); err == nil {
		t.Fatalf("expected error for value > MaxInt")
	}
}

func TestParseNodeListAcceptsENR(t *testing.T) {
	key, _ := crypto.GenerateKey()
	var r enr.Record
	r.Set(enr.IPv4(net.IPv4(1, 2, 3, 4)))
	r.Set(enr.UDP(55555))
	require.NoError(t, enode.SignV4(&r, key))
	n, _ := enode.New(enode.ValidSchemes, &r)

	nodes, err := parseNodeList(n.String() + ",enode://" + hex.EncodeToString(crypto.FromECDSAPub(&key.PublicKey)[1:]) + "@5.6.7.8:11235")
	require.NoError(t, err)
	require.Len(t, nodes, 2)
	require.Zero(t, nodes[0].TCP())
	require.Equal(t, 11235, nodes[1].TCP())
	_, err = parseNodeList("enr:not-base64")
	require.Error(t, err)
}
