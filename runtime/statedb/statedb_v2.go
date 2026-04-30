// Copyright (c) 2026 The VeChainThor developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package statedb

import (
	"github.com/ethereum/go-ethereum/common"

	"github.com/vechain/thor/v2/state"
	"github.com/vechain/thor/v2/thor"
)

// StateDBV2 is the post-INTERSTELLAR 0x02 statedb: it inherits all V1 methods
// and overrides only SetNonce to actually write the account nonce.
// Runtime selects V2 iff the tx is 0x02 and the block is post-INTERSTELLAR.
type StateDBV2 struct {
	*StateDB
}

// NewV2 creates a V2 statedb wrapping the same *state.State as V1.
func NewV2(state *state.State) *StateDBV2 {
	return &StateDBV2{StateDB: New(state)}
}

// SetNonce writes the sequential nonce to state. V1's no-op is shadowed.
func (s *StateDBV2) SetNonce(addr common.Address, nonce uint64) {
	if err := s.state.SetNonce(thor.Address(addr), nonce); err != nil {
		panic(err)
	}
}
