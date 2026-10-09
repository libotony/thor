// Copyright (c) 2026 The VeChainThor developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

// Package forkid implements EIP-2124 style fork identifiers for VeChain:
// Hash = CRC32 over genesis ID and all activated fork heights, Next = the
// first scheduled-but-not-activated fork height (0 if none).
package forkid

import (
	"encoding/binary"
	"errors"
	"hash/crc32"
	"math"
	"slices"

	"github.com/vechain/thor/v2/thor"
)

var (
	// ErrRemoteStale is returned when the remote advertises a past checksum
	// without scheduling the fork that the local chain has already passed.
	ErrRemoteStale = errors.New("remote needs update")
	// ErrLocalIncompatibleOrStale is returned when the remote checksum is
	// incompatible with the local fork history, or the local node itself
	// missed a fork the remote has scheduled and passed.
	ErrLocalIncompatibleOrStale = errors.New("local incompatible or needs update")
)

// ID is the EIP-2124 fork identifier, RLP-encoded as [Hash, Next].
type ID struct {
	Hash [4]byte
	Next uint64
}

// NewID computes the fork identifier at the given head height.
func NewID(genesisID thor.Bytes32, fc *thor.ForkConfig, head uint32) ID {
	hash := crc32.ChecksumIEEE(genesisID[:])
	for _, fork := range gatherForks(fc) {
		if fork <= uint64(head) {
			hash = checksumUpdate(hash, fork)
			continue
		}
		return ID{Hash: checksumToBytes(hash), Next: fork}
	}
	return ID{Hash: checksumToBytes(hash)}
}

// NewFilter returns a validator for remote fork identifiers, evaluated
// against the local head height at call time (EIP-2124 four rules, block
// height only).
func NewFilter(genesisID thor.Bytes32, fc *thor.ForkConfig, head func() uint32) func(ID) error {
	forks := gatherForks(fc)
	sums := make([][4]byte, len(forks)+1)
	hash := crc32.ChecksumIEEE(genesisID[:])
	sums[0] = checksumToBytes(hash)
	for i, fork := range forks {
		hash = checksumUpdate(hash, fork)
		sums[i+1] = checksumToBytes(hash)
	}
	forks = append(forks, math.MaxUint64) // sentinel, never reached by head

	return func(id ID) error {
		h := uint64(head())
		for i, fork := range forks {
			if h >= fork {
				continue
			}
			// forks[i] is the first fork not yet passed locally; sums[i] is our checksum.
			if id.Hash == sums[i] {
				if id.Next > 0 && h >= id.Next {
					return ErrLocalIncompatibleOrStale
				}
				return nil
			}
			for j := range i {
				if id.Hash == sums[j] {
					if forks[j] != id.Next {
						return ErrRemoteStale
					}
					return nil
				}
			}
			for j := i + 1; j < len(sums); j++ {
				if id.Hash == sums[j] {
					return nil
				}
			}
			return ErrLocalIncompatibleOrStale
		}
		return nil // unreachable: sentinel guarantees in-loop return
	}
}

// gatherForks normalizes the fork config into a sorted, deduplicated list of
// activation heights, dropping unscheduled (MaxUint32) and genesis (0) entries.
// Height-0 forks are treated as genesis rules and excluded (per EIP-2124), so a
// fork configured at 0 and one configured as never-scheduled (MaxUint32) yield
// identical fork IDs; cross-checking such custom-network configs is out of
// scope for fork id.
func gatherForks(fc *thor.ForkConfig) []uint64 {
	if fc == nil {
		fc = &thor.NoFork
	}
	// Explicit field list; guarded by TestGatherForksCoversAllFields.
	heights := []uint32{fc.VIP191, fc.ETH_CONST, fc.BLOCKLIST, fc.ETH_IST, fc.VIP214, fc.FINALITY, fc.GALACTICA, fc.HAYABUSA, fc.INTERSTELLAR}
	forks := make([]uint64, 0, len(heights))
	for _, h := range heights {
		if h != math.MaxUint32 && h != 0 {
			forks = append(forks, uint64(h))
		}
	}
	slices.Sort(forks)
	return slices.Compact(forks)
}

func checksumUpdate(hash uint32, fork uint64) uint32 {
	var blob [8]byte
	binary.BigEndian.PutUint64(blob[:], fork)
	return crc32.Update(hash, crc32.IEEETable, blob[:])
}

func checksumToBytes(hash uint32) [4]byte {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], hash)
	return b
}
