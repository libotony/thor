// Copyright (c) 2020 The VeChainThor developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package poa

import (
	"encoding/binary"

	"github.com/ethereum/go-ethereum/rlp"
	"github.com/vechain/thor/block"
	"github.com/vechain/thor/chain"
	"github.com/vechain/thor/thor"
)

var emptyRoot = thor.Blake2b(rlp.EmptyString) // This is the known root hash of an empty trie.

// Seeder generates seed for poa scheduler.
type Seeder struct {
	repo  *chain.Repository
	cache map[thor.Bytes32][]byte
}

// NewSeeder creates a seeder
func NewSeeder(repo *chain.Repository) *Seeder {
	return &Seeder{
		repo,
		make(map[thor.Bytes32][]byte),
	}
}

// Generate creates a seed for the given parent block's header. If the seed block contains at least one backer signature,
// concatenate the VRF outputs(beta) to create seed. Otherwise，returns nil.
func (seeder *Seeder) Generate(parentID thor.Bytes32) ([]byte, error) {
	blockNum := block.Number(parentID) + 1

	round := blockNum / thor.EpochInterval
	if round < 1 {
		return nil, nil
	}
	seedNum := (round - 1) * thor.EpochInterval

	seedBlock, err := seeder.repo.NewChain(parentID).GetBlockHeader(seedNum)
	if err != nil {
		return nil, err
	}

	if v, ok := seeder.cache[seedBlock.ID()]; ok == true {
		return append([]byte(nil), v...), nil
	}

	signer, err := seedBlock.Signer()
	if err != nil {
		return nil, err
	}

	var seed []byte
	if seedBlock.BackerSignaturesRoot() != emptyRoot {
		previousSeed, err := seeder.Generate(seedBlock.ParentID())
		if err != nil {
			return nil, err
		}

		// block declaration as message
		// [parentID + txsRoot + gaslimit + timestamp + signer]
		msg := make([]byte, 100)
		copy(msg[:], seedBlock.ParentID().Bytes())
		copy(msg[32:], seedBlock.TxsRoot().Bytes())
		binary.BigEndian.PutUint64(msg[64:], seedBlock.GasLimit())
		binary.BigEndian.PutUint64(msg[72:], seedBlock.Timestamp())
		copy(msg[80:], signer.Bytes())

		var (
			data []byte
			num  [4]byte
		)
		binary.BigEndian.PutUint32(num[:], block.Number(parentID))
		data = append(data, previousSeed...)
		data = append(data, num[:]...)

		bss, err := seeder.repo.GetBlockBackerSignatures(seedBlock.ID())
		if err != nil {
			return nil, err
		}
		for _, bs := range bss {
			_, pub, err := bs.Signer(msg)
			if err != nil {
				return nil, err
			}
			beta, err := bs.Validate(pub, thor.Blake2b(data))
			if err != nil {
				return nil, err
			}
			seed = append(seed, beta...)
		}
	} else {
		t := make([]byte, 8)
		binary.BigEndian.PutUint64(t, seedBlock.TotalBackersCount())

		seed = append(seed, signer.Bytes()...)
		seed = append(seed, t...)
	}

	seeder.cache[seedBlock.ID()] = seed
	return seed, nil
}
