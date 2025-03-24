// Copyright (c) 2024 The VeChainThor developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package tx

import (
	"io"
	"math"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/vechain/thor/v2/thor"
)

type dynamicFeeTransaction struct {
	ChainTag             byte
	BlockRef             uint64
	Expiration           uint32
	Clauses              []*Clause
	Gas                  uint64
	MaxFeePerGas         *big.Int
	MaxPriorityFeePerGas *big.Int
	DependsOn            *thor.Bytes32 `rlp:"nil"`
	Nonce                uint64
	Reserved             reserved
	Signature            []byte
}

func (t *dynamicFeeTransaction) txType() byte {
	return TypeDynamicFee
}

func (t *dynamicFeeTransaction) copy() txData {
	cpy := &dynamicFeeTransaction{
		ChainTag:             t.ChainTag,
		BlockRef:             t.BlockRef,
		Expiration:           t.Expiration,
		Clauses:              make([]*Clause, len(t.Clauses)),
		Gas:                  t.Gas,
		MaxFeePerGas:         new(big.Int),
		MaxPriorityFeePerGas: new(big.Int),
		DependsOn:            t.DependsOn,
		Nonce:                t.Nonce,
		Reserved:             t.Reserved,
		Signature:            t.Signature,
	}
	copy(cpy.Clauses, t.Clauses)
	if t.MaxFeePerGas != nil {
		cpy.MaxFeePerGas.Set(t.MaxFeePerGas)
	}
	if t.MaxPriorityFeePerGas != nil {
		cpy.MaxPriorityFeePerGas.Set(t.MaxPriorityFeePerGas)
	}
	return cpy
}

func (t *dynamicFeeTransaction) chainTag() byte {
	return t.ChainTag
}

func (t *dynamicFeeTransaction) blockRef() uint64 {
	return t.BlockRef
}

func (t *dynamicFeeTransaction) expiration() uint32 {
	return t.Expiration
}

func (t *dynamicFeeTransaction) clauses() []*Clause {
	return t.Clauses
}

func (t *dynamicFeeTransaction) gas() uint64 {
	return t.Gas
}

func (t *dynamicFeeTransaction) gasPriceCoef() uint8 {
	if t.MaxFeePerGas.Cmp(big.NewInt(math.MaxUint8)) > 0 {
		return math.MaxUint8
	}
	return uint8(t.MaxFeePerGas.Uint64())
}

func (t *dynamicFeeTransaction) maxFeePerGas() *big.Int {
	if t.MaxFeePerGas == nil {
		return common.Big0
	}
	return new(big.Int).Set(t.MaxFeePerGas)
}

func (t *dynamicFeeTransaction) maxPriorityFeePerGas() *big.Int {
	if t.MaxPriorityFeePerGas == nil {
		return common.Big0
	}
	return new(big.Int).Set(t.MaxPriorityFeePerGas)
}

func (t *dynamicFeeTransaction) dependsOn() *thor.Bytes32 {
	return t.DependsOn
}

func (t *dynamicFeeTransaction) nonce() uint64 {
	return t.Nonce
}

func (t *dynamicFeeTransaction) reserved() reserved {
	return t.Reserved
}

func (t *dynamicFeeTransaction) signature() []byte {
	return t.Signature
}

func (t *dynamicFeeTransaction) setSignature(sig []byte) {
	t.Signature = sig
}

func (t *dynamicFeeTransaction) hashWithoutNonce(origin thor.Address) *thor.Bytes32 {
	b := thor.Blake2bFn(func(w io.Writer) {
		rlp.Encode(w, []any{
			t.chainTag(),
			t.blockRef(),
			t.expiration(),
			t.clauses(),
			t.maxFeePerGas(),
			t.maxPriorityFeePerGas(),
			t.dependsOn(),
			t.nonce(),
			t.reserved(),
			origin,
		})
	})
	return &b
}

func (t *dynamicFeeTransaction) encode(w io.Writer) error {
	return rlp.Encode(w, []any{
		t.ChainTag,
		t.BlockRef,
		t.Expiration,
		t.Clauses,
		t.Gas,
		t.MaxFeePerGas,
		t.MaxPriorityFeePerGas,
		t.DependsOn,
		t.Nonce,
		&t.Reserved,
	})
}

func (t *dynamicFeeTransaction) evaluateWork(origin thor.Address) func(nonce uint64) *big.Int {
	return func(nonce uint64) *big.Int { return common.Big0 }
}
