//go:build go1.18

package tests

import (
	"testing"

	"github.com/ethereum/go-ethereum/rlp"
	"github.com/vechain/thor/block"
)

func FuzzBlock(f *testing.F) {
	f.Fuzz(func(t *testing.T, raw []byte) {
		var blk block.Block
		rlp.DecodeBytes(raw, &blk)
	})
}
