// Copyright (c) 2021 The VeChainThor developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package vm

import (
	"math/big"

	"github.com/vechain/thor/v2/vm/internal"
)

// isForked returns whether a fork scheduled at block s is active at the given head block.
func isForked(s, head *big.Int) bool {
	if s == nil || head == nil {
		return false
	}
	return s.Cmp(head) <= 0
}

// ChainConfig holds the fork schedule the EVM needs. Field names follow go-ethereum's params.ChainConfig.
type ChainConfig struct {
	ChainID *big.Int

	HomesteadBlock      *big.Int
	EIP150Block         *big.Int
	EIP155Block         *big.Int
	EIP158Block         *big.Int
	ByzantiumBlock      *big.Int
	ConstantinopleBlock *big.Int
	IstanbulBlock       *big.Int // Istanbul switch block (nil = no fork, 0 = already on istanbul)
	ShanghaiBlock       *big.Int // Shanghai switch block (nil = no fork, 0 = already on shanghai)
	OsakaBlock          *big.Int // Osaka switch block (nil = no fork, 0 = already on osaka)
}

// IsHomestead returns whether num is either equal to the Homestead fork block or greater.
func (c *ChainConfig) IsHomestead(num *big.Int) bool {
	return isForked(c.HomesteadBlock, num)
}

// IsEIP150 returns whether num is either equal to the EIP150 fork block or greater.
func (c *ChainConfig) IsEIP150(num *big.Int) bool {
	return isForked(c.EIP150Block, num)
}

// IsEIP155 returns whether num is either equal to the EIP155 fork block or greater.
func (c *ChainConfig) IsEIP155(num *big.Int) bool {
	return isForked(c.EIP155Block, num)
}

// IsEIP158 returns whether num is either equal to the EIP158 fork block or greater.
func (c *ChainConfig) IsEIP158(num *big.Int) bool {
	return isForked(c.EIP158Block, num)
}

// IsByzantium returns whether num is either equal to the Byzantium fork block or greater.
func (c *ChainConfig) IsByzantium(num *big.Int) bool {
	return isForked(c.ByzantiumBlock, num)
}

// IsConstantinople returns whether num is either equal to the Constantinople fork block or greater.
func (c *ChainConfig) IsConstantinople(num *big.Int) bool {
	return isForked(c.ConstantinopleBlock, num)
}

// IsIstanbul returns whether num is either equal to the Istanbul fork block or greater.
func (c *ChainConfig) IsIstanbul(num *big.Int) bool {
	return isForked(c.IstanbulBlock, num)
}

// IsShanghai returns whether num is either equal to the Shanghai fork block or greater.
func (c *ChainConfig) IsShanghai(num *big.Int) bool {
	return isForked(c.ShanghaiBlock, num)
}

// IsOsaka returns whether num is either equal to the Osaka fork block or greater.
func (c *ChainConfig) IsOsaka(num *big.Int) bool {
	return isForked(c.OsakaBlock, num)
}

// GasTable returns the gas table for the given block number.
func (c *ChainConfig) GasTable(num *big.Int) internal.GasTable {
	switch {
	case c.IsConstantinople(num):
		return internal.GasTableConstantinople
	case c.IsEIP158(num):
		return internal.GasTableEIP158
	case c.IsEIP150(num):
		return internal.GasTableEIP150
	default:
		return internal.GasTableHomestead
	}
}

// Rules wraps ChainConfig and is merely syntatic sugar or can be used for functions
// that do not have or require information about the block.
//
// Rules is a one time interface meaning that it shouldn't be used in between transition
// phases.
type Rules struct {
	ChainID                                   *big.Int
	IsHomestead, IsEIP150, IsEIP155, IsEIP158 bool
	IsByzantium                               bool
	IsIstanbul                                bool
	IsShanghai                                bool
	IsOsaka                                   bool
}

// Rules ensures c's ChainID is not nil.
func (c *ChainConfig) Rules(num *big.Int) Rules {
	chainID := c.ChainID
	if chainID == nil {
		chainID = new(big.Int)
	}
	return Rules{
		ChainID:     new(big.Int).Set(chainID),
		IsHomestead: c.IsHomestead(num),
		IsEIP150:    c.IsEIP150(num),
		IsEIP155:    c.IsEIP155(num),
		IsEIP158:    c.IsEIP158(num),
		IsByzantium: c.IsByzantium(num),
		IsIstanbul:  c.IsIstanbul(num),
		IsShanghai:  c.IsShanghai(num),
		IsOsaka:     c.IsOsaka(num),
	}
}
