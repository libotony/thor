// Copyright (c) 2026 The VeChainThor developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package vm

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/params"
	"github.com/stretchr/testify/assert"
)

// testChainConfig matches params.TestChainConfig from the vechain/go-ethereum fork thor used before
// switching to upstream (Constantinople off).
func testChainConfig() *ChainConfig {
	return &ChainConfig{
		ChainID:        big.NewInt(1),
		HomesteadBlock: big.NewInt(0),
		EIP150Block:    big.NewInt(0),
		EIP155Block:    big.NewInt(0),
		EIP158Block:    big.NewInt(0),
		ByzantiumBlock: big.NewInt(0),
	}
}

func TestForkChecksMatchUpstream(t *testing.T) {
	forks := []struct {
		name  string
		set   func(*ChainConfig, *params.ChainConfig, *big.Int)
		thor  func(*ChainConfig, *big.Int) bool
		upstr func(*params.ChainConfig, *big.Int) bool
	}{
		{
			"Homestead", func(c *ChainConfig, u *params.ChainConfig, h *big.Int) { c.HomesteadBlock, u.HomesteadBlock = h, h },
			(*ChainConfig).IsHomestead, (*params.ChainConfig).IsHomestead,
		},
		{
			"EIP150", func(c *ChainConfig, u *params.ChainConfig, h *big.Int) { c.EIP150Block, u.EIP150Block = h, h },
			(*ChainConfig).IsEIP150, (*params.ChainConfig).IsEIP150,
		},
		{
			"EIP155", func(c *ChainConfig, u *params.ChainConfig, h *big.Int) { c.EIP155Block, u.EIP155Block = h, h },
			(*ChainConfig).IsEIP155, (*params.ChainConfig).IsEIP155,
		},
		{
			"EIP158", func(c *ChainConfig, u *params.ChainConfig, h *big.Int) { c.EIP158Block, u.EIP158Block = h, h },
			(*ChainConfig).IsEIP158, (*params.ChainConfig).IsEIP158,
		},
		{
			"Byzantium", func(c *ChainConfig, u *params.ChainConfig, h *big.Int) { c.ByzantiumBlock, u.ByzantiumBlock = h, h },
			(*ChainConfig).IsByzantium, (*params.ChainConfig).IsByzantium,
		},
		{
			"Constantinople", func(c *ChainConfig, u *params.ChainConfig, h *big.Int) {
				c.ConstantinopleBlock, u.ConstantinopleBlock = h, h
			},
			(*ChainConfig).IsConstantinople, (*params.ChainConfig).IsConstantinople,
		},
	}
	heights := []*big.Int{nil, big.NewInt(0), big.NewInt(50), big.NewInt(100)}
	blocks := []*big.Int{nil, big.NewInt(0), big.NewInt(1), big.NewInt(50), big.NewInt(99), big.NewInt(100), big.NewInt(101)}

	for _, f := range forks {
		for _, h := range heights {
			c, u := &ChainConfig{}, &params.ChainConfig{}
			f.set(c, u, h)
			for _, n := range blocks {
				assert.Equal(t, f.upstr(u, n), f.thor(c, n), "%s fork=%v block=%v", f.name, h, n)
			}
		}
	}
}
