// Copyright (c) 2026 The VeChainThor developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package forkid

import (
	"encoding/binary"
	"encoding/hex"
	"hash/crc32"
	"math"
	"reflect"
	"testing"

	"github.com/ethereum/go-ethereum/rlp"
	"github.com/stretchr/testify/assert"

	"github.com/vechain/thor/v2/thor"
)

var (
	mainnetID = thor.MustParseBytes32("0x00000000851caf3cfdb6e899cf5958bfb1ac3413d346d43539627e6be7ec1b4a")
	testnetID = thor.MustParseBytes32("0x000000000b2bce3c70bc649a02749e8687721b09ed2e15997f466536b20bb127")
)

func TestNewIDMainnet(t *testing.T) {
	fc := thor.GetForkConfig(mainnetID)
	cases := []struct {
		head uint32
		hash [4]byte
		next uint64
	}{
		{0, [4]byte{0xe1, 0x77, 0xcc, 0x1e}, 3_337_300},
		{3_337_299, [4]byte{0xe1, 0x77, 0xcc, 0x1e}, 3_337_300},
		{3_337_300, [4]byte{0xbd, 0x1f, 0xc2, 0x6a}, 4_817_300}, // VIP191+ETH_CONST deduplicated into a single update
		{4_817_299, [4]byte{0xbd, 0x1f, 0xc2, 0x6a}, 4_817_300},
		{4_817_300, [4]byte{0xcf, 0xed, 0xd9, 0x4b}, 9_254_300},
		{9_254_300, [4]byte{0x5c, 0x74, 0xa5, 0x18}, 10_653_500},
		{10_653_500, [4]byte{0xfb, 0x37, 0x15, 0x2e}, 13_815_000},
		{13_815_000, [4]byte{0xfb, 0xe9, 0x69, 0xc7}, 22_084_200},
		{22_084_200, [4]byte{0x7c, 0xfa, 0x1a, 0xff}, 23_414_400},
		{23_414_400, [4]byte{0xb5, 0xd7, 0x28, 0xb3}, 25_902_540},
		{25_902_539, [4]byte{0xb5, 0xd7, 0x28, 0xb3}, 25_902_540},
		{25_902_540, [4]byte{0xbb, 0xf3, 0x87, 0x04}, 0},
		{math.MaxUint32, [4]byte{0xbb, 0xf3, 0x87, 0x04}, 0},
	}
	for _, c := range cases {
		id := NewID(mainnetID, fc, c.head)
		assert.Equal(t, ID{Hash: c.hash, Next: c.next}, id, "head=%d", c.head)
	}
}

func TestNewIDTestnet(t *testing.T) {
	fc := thor.GetForkConfig(testnetID)
	cases := []struct {
		head uint32
		hash [4]byte
		next uint64
	}{
		{0, [4]byte{0x0d, 0xe9, 0x4a, 0x54}, 2_898_800},
		{2_898_800, [4]byte{0xc0, 0x0e, 0xa7, 0x19}, 3_192_500},
		{3_192_500, [4]byte{0x0d, 0x3d, 0x0a, 0x19}, 9_146_700}, // BLOCKLIST=MaxUint32 excluded
		{9_146_700, [4]byte{0x18, 0x82, 0x38, 0x64}, 10_606_800},
		{10_606_800, [4]byte{0xbe, 0x6f, 0x59, 0x19}, 13_086_360},
		{13_086_360, [4]byte{0xbc, 0x7e, 0x58, 0x23}, 21_770_500},
		{21_770_500, [4]byte{0xe6, 0xf2, 0x44, 0xc2}, 23_221_800},
		{23_221_800, [4]byte{0x38, 0x90, 0x6e, 0x27}, 25_891_380},
		{25_891_379, [4]byte{0x38, 0x90, 0x6e, 0x27}, 25_891_380},
		{25_891_380, [4]byte{0xcd, 0xb3, 0x71, 0xf8}, 0},
	}
	for _, c := range cases {
		id := NewID(testnetID, fc, c.head)
		assert.Equal(t, ID{Hash: c.hash, Next: c.next}, id, "head=%d", c.head)
	}
}

func TestGatherForks(t *testing.T) {
	// mainnet: VIP191==ETH_CONST deduplicated, 8 heights
	main := gatherForks(thor.GetForkConfig(mainnetID))
	assert.Equal(t, []uint64{3_337_300, 4_817_300, 9_254_300, 10_653_500, 13_815_000, 22_084_200, 23_414_400, 25_902_540}, main)
	// testnet: BLOCKLIST excluded, 8 heights
	test := gatherForks(thor.GetForkConfig(testnetID))
	assert.Equal(t, []uint64{2_898_800, 3_192_500, 9_146_700, 10_606_800, 13_086_360, 21_770_500, 23_221_800, 25_891_380}, test)
	// NoFork excludes everything; nil is treated as NoFork
	assert.Empty(t, gatherForks(&thor.NoFork))
	assert.Empty(t, gatherForks(nil))
	// SoloFork today: INTERSTELLAR=1, the rest 0 -> single-element table
	assert.Equal(t, []uint64{1}, gatherForks(&thor.SoloFork))
}

func TestNewIDSoloAndNil(t *testing.T) {
	g := thor.MustParseBytes32("0x0000000000000000000000000000000000000000000000000000000000000001")
	// nil config → {CRC32(genesisID), 0}
	id := NewID(g, nil, 0)
	assert.Equal(t, uint64(0), id.Next)
	assert.Equal(t, crc32.ChecksumIEEE(g[:]), binary.BigEndian.Uint32(id.Hash[:]))
	// SoloFork: Next=1 at head=0; Next=0 and Hash changes from head>=1
	id0 := NewID(g, &thor.SoloFork, 0)
	assert.Equal(t, uint64(1), id0.Next)
	id1 := NewID(g, &thor.SoloFork, 1)
	assert.Equal(t, uint64(0), id1.Next)
	assert.NotEqual(t, id0.Hash, id1.Hash)
}

func TestGatherForksCoversAllFields(t *testing.T) {
	// reflection guard: fails when a ForkConfig field is added without registering it in gatherForks
	want := []string{"VIP191", "ETH_CONST", "BLOCKLIST", "ETH_IST", "VIP214", "FINALITY", "GALACTICA", "HAYABUSA", "INTERSTELLAR"}
	typ := reflect.TypeFor[thor.ForkConfig]()
	got := make([]string, 0, typ.NumField())
	for f := range typ.Fields() {
		got = append(got, f.Name)
	}
	assert.Equal(t, want, got)

	// behavioral guard: every field must contribute its height to gatherForks
	fc := &thor.ForkConfig{
		VIP191: 10, ETH_CONST: 20, BLOCKLIST: 30, ETH_IST: 40, VIP214: 50,
		FINALITY: 60, GALACTICA: 70, HAYABUSA: 80, INTERSTELLAR: 90,
	}
	assert.Equal(t, []uint64{10, 20, 30, 40, 50, 60, 70, 80, 90}, gatherForks(fc))
}

// Vectors from geth core/forkid TestEncoding.
func TestIDRLPEncoding(t *testing.T) {
	cases := []struct {
		id   ID
		want string
	}{
		{ID{Hash: checksumToBytes(0), Next: 0}, "c6840000000080"},
		{ID{Hash: checksumToBytes(0xdeadbeef), Next: 0xBADDCAFE}, "ca84deadbeef84baddcafe"},
		{ID{Hash: checksumToBytes(math.MaxUint32), Next: math.MaxUint64}, "ce84ffffffff88ffffffffffffffff"},
	}
	for i, c := range cases {
		enc, err := rlp.EncodeToBytes(c.id)
		assert.NoError(t, err, "case %d", i)
		assert.Equal(t, c.want, hex.EncodeToString(enc), "case %d", i)

		var dec ID
		assert.NoError(t, rlp.DecodeBytes(enc, &dec), "case %d", i)
		assert.Equal(t, c.id, dec, "case %d", i)
	}
}

func TestFilter(t *testing.T) {
	fc := thor.GetForkConfig(mainnetID)
	filterAt := func(head uint32) func(ID) error {
		return NewFilter(mainnetID, fc, func() uint32 { return head })
	}
	cur := uint32(25_902_540) // INTERSTELLAR activated
	cases := []struct {
		name string
		head uint32
		id   ID
		want error
	}{
		// rule 1b: same window, the normal case
		{"same window", cur, ID{[4]byte{0xbb, 0xf3, 0x87, 0x04}, 0}, nil},
		// rule 1b: remote knows a future fork that has not triggered yet
		{"remote knows future fork", cur, ID{[4]byte{0xbb, 0xf3, 0x87, 0x04}, 30_000_000}, nil},
		// rule 1a: local already passed a fork the remote scheduled but does not have it -> local stale
		{"local stale", 25_902_541, ID{[4]byte{0xbb, 0xf3, 0x87, 0x04}, 25_902_541}, ErrLocalIncompatibleOrStale},
		// rule 2: remote is lagging but has the next fork scheduled correctly
		{"remote lagging ok", cur, ID{[4]byte{0xb5, 0xd7, 0x28, 0xb3}, 25_902_540}, nil},
		// rule 2: remote is stuck in an old window without the fork scheduled -> stale peer (eviction path)
		{"remote stale", cur, ID{[4]byte{0xb5, 0xd7, 0x28, 0xb3}, 0}, ErrRemoteStale},
		{"remote very old ok", cur, ID{[4]byte{0xe1, 0x77, 0xcc, 0x1e}, 3_337_300}, nil},
		{"remote very old bad next", cur, ID{[4]byte{0xe1, 0x77, 0xcc, 0x1e}, 42}, ErrRemoteStale},
		// rule 1: local is before a scheduled but not yet activated fork
		{"pre-fork remote unaware", 25_000_000, ID{[4]byte{0xb5, 0xd7, 0x28, 0xb3}, 0}, nil},
		{"pre-fork remote upgraded", 25_000_000, ID{[4]byte{0xb5, 0xd7, 0x28, 0xb3}, 25_902_540}, nil},
		{"pre-fork remote bogus fork", 25_000_000, ID{[4]byte{0xb5, 0xd7, 0x28, 0xb3}, 24_000_000}, ErrLocalIncompatibleOrStale},
		// rule 3: local is lagging, remote is in a future window
		{"local lagging", 23_414_400, ID{[4]byte{0xbb, 0xf3, 0x87, 0x04}, 0}, nil},
		// rule 4: different chain
		{"different chain", cur, ID{[4]byte{0xde, 0xad, 0xbe, 0xef}, 0}, ErrLocalIncompatibleOrStale},
		// an absurd Next must not panic and must yield a deterministic verdict
		{"absurd next", cur, ID{[4]byte{0xbb, 0xf3, 0x87, 0x04}, math.MaxUint64}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.ErrorIs(t, filterAt(c.head)(c.id), c.want)
		})
	}
}

func TestFilterNilConfig(t *testing.T) {
	// nil config degrades to a pure genesis check
	g := thor.MustParseBytes32("0x0000000000000000000000000000000000000000000000000000000000000002")
	f := NewFilter(g, nil, func() uint32 { return 100 })
	assert.NoError(t, f(NewID(g, nil, 0)))
	assert.ErrorIs(t, f(ID{[4]byte{1, 2, 3, 4}, 0}), ErrLocalIncompatibleOrStale)
	// rule 1a: same genesis, but the remote scheduled a fork the local already passed
	var genesisSum [4]byte
	binary.BigEndian.PutUint32(genesisSum[:], crc32.ChecksumIEEE(g[:]))
	assert.ErrorIs(t, f(ID{genesisSum, 50}), ErrLocalIncompatibleOrStale)
}
