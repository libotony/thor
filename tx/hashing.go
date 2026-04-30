// Copyright (c) 2024 The VeChainThor developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package tx

import (
	"bytes"
	"io"
	"sync"

	"github.com/vechain/thor/v2/rlp"

	"github.com/vechain/thor/v2/thor"
)

// encodeBufferPool holds temporary encoder buffers for tx encoding.
var encodeBufferPool = sync.Pool{
	New: func() any { return new(bytes.Buffer) },
}

func rlpHash(x any) thor.Bytes32 {
	return thor.Blake2bFn(func(w io.Writer) {
		rlp.Encode(w, x)
	})
}

// prefixedRlpHash returns Blake2b(prefix || RLP(x)). Used for typed VeChain
// transactions (0x51).
func prefixedRlpHash(prefix byte, x any) thor.Bytes32 {
	return thor.Blake2bFn(func(w io.Writer) {
		w.Write([]byte{prefix})
		rlp.Encode(w, x)
	})
}

// keccakPrefixedRlpHash returns Keccak256(prefix || RLP(x)) — the eth canonical
// txhash shape used by 0x02 (EIP-1559).
func keccakPrefixedRlpHash(prefix byte, x any) thor.Bytes32 {
	return thor.Keccak256Fn(func(w io.Writer) {
		w.Write([]byte{prefix})
		rlp.Encode(w, x)
	})
}
