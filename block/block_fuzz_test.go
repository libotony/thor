// Copyright (c) 2023 The VeChainThor developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>
//go:build go1.18

package block

import (
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/vechain/thor/thor"
	"github.com/vechain/thor/tx"
)

func FuzzBlock(f *testing.F) {
	tx1 := new(tx.Builder).Clause(tx.NewClause(&thor.Address{})).Clause(tx.NewClause(&thor.Address{})).Build()
	tx2 := new(tx.Builder).Clause(tx.NewClause(nil)).Build()
	privKey := string("dce1443bd2ef0c2631adc1c67e5c93f13dc23a41c18b536effbbdcbcdb96fb65")
	now := uint64(time.Now().UnixNano())

	var (
		gasUsed     uint64       = 1000
		gasLimit    uint64       = 14000
		totalScore  uint64       = 101
		emptyRoot   thor.Bytes32 = thor.BytesToBytes32([]byte("0"))
		beneficiary thor.Address = thor.BytesToAddress([]byte("abc"))
	)

	blk := new(Builder).
		GasUsed(gasUsed).
		Transaction(tx1).
		Transaction(tx2).
		GasLimit(gasLimit).
		TotalScore(totalScore).
		StateRoot(emptyRoot).
		ReceiptsRoot(emptyRoot).
		Timestamp(now).
		ParentID(emptyRoot).
		Beneficiary(beneficiary).
		Build()

	key, _ := crypto.HexToECDSA(privKey)
	sig, _ := crypto.Sign(blk.Header().SigningHash().Bytes(), key)
	blk = blk.WithSignature(sig)
	data, _ := rlp.EncodeToBytes(blk)

	f.Add(data)
	f.Fuzz(func(t *testing.T, orig []byte) {
		var decodeBlock Block
		rlp.DecodeBytes(orig, &decodeBlock)
	})
}
