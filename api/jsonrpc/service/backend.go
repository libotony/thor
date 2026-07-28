// Copyright (c) 2026 The VeChainThor developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package service

import (
	"strings"

	"github.com/vechain/thor/v2/api/restutil"
	"github.com/vechain/thor/v2/bft"
	"github.com/vechain/thor/v2/chain"
	"github.com/vechain/thor/v2/state"
)

// Backend holds the thor chain dependencies shared by the JSON-RPC services.
type Backend struct {
	repo   *chain.Repository
	stater *state.Stater
	bft    bft.Committer
}

// NewBackend creates a Backend shared by the JSON-RPC services.
func NewBackend(repo *chain.Repository, stater *state.Stater, bft bft.Committer) *Backend {
	return &Backend{repo: repo, stater: stater, bft: bft}
}

// summaryAt resolves an eth block parameter (tag, hex number or block hash; nil means latest).
func (b *Backend) summaryAt(blockTag *string) (*chain.BlockSummary, error) {
	tag := "latest"
	if blockTag != nil {
		tag = *blockTag
	}
	var rev string
	switch tag {
	case "latest", "pending":
		rev = "best"
	case "earliest":
		rev = "0"
	case "safe":
		rev = "justified"
	case "finalized":
		rev = "finalized"
	default:
		// only eth vocabulary is accepted: 0x-prefixed block number or block hash
		if !strings.HasPrefix(tag, "0x") {
			return nil, invalidParamsError{"invalid block tag"}
		}
		rev = tag
	}
	r, err := restutil.ParseRevision(rev, false)
	if err != nil {
		return nil, invalidParamsError{err.Error()}
	}
	sum, err := restutil.GetSummary(r, b.repo, b.bft)
	if err != nil {
		if b.repo.IsNotFound(err) || err.Error() == "invalid revision" {
			// "invalid revision" is GetSummary's zero-hash case; both are absent blocks
			return nil, errHeaderNotFound
		}
		return nil, err
	}
	return sum, nil
}
