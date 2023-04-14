// Copyright (c) 2023 The VeChainThor developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>
//go:build go1.18

package tx

import (
	"encoding/hex"
	"testing"

	"github.com/ethereum/go-ethereum/rlp"
)

func FuzzTransaction(f *testing.F) {
	raw, _ := hex.DecodeString("f8db81a484aabbccdd20f840df947567d83b7b8d80addcb281a71d54fc7b3364ffed82271086000000606060df947567d83b7b8d80addcb281a71d54fc7b3364ffed824e20860000006060608180830334508083bc614ec101b882bad4d4401b1fb1c41d61727d7fd2aeb2bb3e65a27638a5326ca98404c0209ab159eaeb37f0ac75ed1ac44d92c3d17402d7d64b4c09664ae2698e1102448040c000f043fafeaf60343248a37e4f1d2743b4ab9116df6d627b4d8a874e4f48d3ae671c4e8d136eb87c544bea1763673a5f1762c2266364d1b22166d16e3872b5a9c700")
	f.Add(raw)

	f.Fuzz(func(t *testing.T, orig []byte) {
		var newTx *Transaction
		_ = rlp.DecodeBytes(orig, &newTx)
	})
}
