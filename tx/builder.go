// Copyright (c) 2024 The VeChainThor developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package tx

import (
	"encoding/binary"
	"math/big"

	"github.com/vechain/thor/v2/thor"
)

// Builder to make it easy to build transaction.
type Builder struct {
	txType               Type
	chainTag             byte
	clauses              []*Clause
	gasPriceCoef         uint8
	maxFeePerGas         *big.Int
	maxPriorityFeePerGas *big.Int
	gas                  uint64
	blockRef             uint64
	expiration           uint32
	nonce                uint64
	dependsOn            *thor.Bytes32
	reserved             reserved

	// 0x02 (ETH EIP-1559) specific. Ignored for other tx types.
	// (To, Value, Data) come from clauses[0] — see Build().
	chainID uint64
}

func NewBuilder(txType Type) *Builder {
	return &Builder{txType: txType}
}

// ChainTag set chain tag.
func (b *Builder) ChainTag(tag byte) *Builder {
	b.chainTag = tag
	return b
}

// Clause add a clause.
func (b *Builder) Clause(c *Clause) *Builder {
	b.clauses = append(b.clauses, c)
	return b
}

func (b *Builder) Clauses(clauses []*Clause) *Builder {
	for _, c := range clauses {
		b.Clause(c)
	}
	return b
}

// GasPriceCoef set gas price coef.
func (b *Builder) GasPriceCoef(coef uint8) *Builder {
	b.gasPriceCoef = coef
	return b
}

// MaxFeePerGas set max fee per gas.
func (b *Builder) MaxFeePerGas(maxFeePerGas *big.Int) *Builder {
	b.maxFeePerGas = maxFeePerGas
	return b
}

// MaxPriorityFeePerGas set max priority fee per gas.
func (b *Builder) MaxPriorityFeePerGas(maxPriorityFeePerGas *big.Int) *Builder {
	b.maxPriorityFeePerGas = maxPriorityFeePerGas
	return b
}

// Gas set gas provision for tx.
func (b *Builder) Gas(gas uint64) *Builder {
	b.gas = gas
	return b
}

// BlockRef set block reference.
func (b *Builder) BlockRef(br BlockRef) *Builder {
	b.blockRef = binary.BigEndian.Uint64(br[:])
	return b
}

// Expiration set expiration.
func (b *Builder) Expiration(exp uint32) *Builder {
	b.expiration = exp
	return b
}

// Nonce set nonce.
func (b *Builder) Nonce(nonce uint64) *Builder {
	b.nonce = nonce
	return b
}

// DependsOn set depended tx.
func (b *Builder) DependsOn(txID *thor.Bytes32) *Builder {
	if txID == nil {
		b.dependsOn = nil
	} else {
		cpy := *txID
		b.dependsOn = &cpy
	}
	return b
}

// Features set features.
func (b *Builder) Features(feat Features) *Builder {
	b.reserved.Features = feat
	return b
}

// ChainID sets the Ethereum chainID used by type 0x02 transactions. Ignored
// for non-0x02 types.
func (b *Builder) ChainID(chainID uint64) *Builder {
	b.chainID = chainID
	return b
}

// Build builds a tx object.
func (b *Builder) Build() *Transaction {
	if b.txType == TypeLegacy {
		return &Transaction{
			body: &legacyTransaction{
				ChainTag:     b.chainTag,
				Clauses:      b.clauses,
				GasPriceCoef: b.gasPriceCoef,
				Gas:          b.gas,
				BlockRef:     b.blockRef,
				Expiration:   b.expiration,
				Nonce:        b.nonce,
				DependsOn:    b.dependsOn,
				Reserved:     b.reserved,
			},
		}
	}

	if b.txType == TypeEthDynamicFee {
		// 0x02 carries exactly one (to, value, data) tuple at the envelope
		// level. We model that with the existing single-clause API instead
		// of duplicating it as separate Eth{To,Value,Data} fields, so the
		// builder shape matches how the runtime resolves 0x02 (single
		// Clause for execution). Multi-clause is rejected here rather than
		// being silently truncated.
		if len(b.clauses) != 1 {
			panic("tx: TypeEthDynamicFee requires exactly one clause")
		}
		c := b.clauses[0]

		value := c.body.Value
		if value == nil {
			value = new(big.Int)
		}
		maxFee := b.maxFeePerGas
		if maxFee == nil {
			maxFee = new(big.Int)
		}
		maxPrio := b.maxPriorityFeePerGas
		if maxPrio == nil {
			maxPrio = new(big.Int)
		}
		return &Transaction{
			body: &ethDynamicFeeTransaction{
				ChainID:              new(big.Int).SetUint64(b.chainID),
				Nonce:                b.nonce,
				MaxPriorityFeePerGas: maxPrio,
				MaxFeePerGas:         maxFee,
				Gas:                  b.gas,
				To:                   c.body.To,
				Value:                new(big.Int).Set(value),
				Data:                 append([]byte(nil), c.body.Data...),
				// AccessList: builder always emits the canonical empty
				// list (RLP zero-length slice). Non-empty lists are
				// rejected at runtime; round-trip parity with non-empty
				// lists is exercised via direct RLP decode in
				// TestEthDynamicFee_DecodePreservesAccessList, not the
				// builder.
				V: new(big.Int),
				R: new(big.Int),
				S: new(big.Int),
			},
		}
	}

	return &Transaction{
		body: &dynamicFeeTransaction{
			ChainTag:             b.chainTag,
			Clauses:              b.clauses,
			MaxFeePerGas:         b.maxFeePerGas,
			MaxPriorityFeePerGas: b.maxPriorityFeePerGas,
			Gas:                  b.gas,
			BlockRef:             b.blockRef,
			Expiration:           b.expiration,
			Nonce:                b.nonce,
			DependsOn:            b.dependsOn,
			Reserved:             b.reserved,
		},
	}
}
