// Copyright (c) 2026 The VeChainThor developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

// VeChain-defined ENR entry.
//
// Node records (EIP-778) published by thor carry one VeChain-specific key:
//
//	"vet" => [[hash: 4 bytes, next: uint64], ...]
//
// hash and next are the EIP-2124 fork id of the publishing node (package forkid):
// hash chains the genesis ID and every activated fork height, next is the next
// scheduled fork height or 0. The outer list leaves room for future fields; readers
// keep and ignore anything after the fork id. Same shape as geth's "eth" entry.
//
// The entry is written into the local record at startup through Protocol.Attributes
// and re-published by the updater when the local head crosses a fork height.
// Discovery candidates without a compatible "vet" entry are not dialed.

package comm

import (
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/p2p/enr"
	"github.com/ethereum/go-ethereum/rlp"

	"github.com/vechain/thor/v2/forkid"
)

const enrKey = "vet"

// enrEntry is the VeChain ENR entry; see the file comment for the wire format.
type enrEntry struct {
	ForkID forkid.ID
	Rest   []rlp.RawValue `rlp:"tail"` // forward compatibility: trailing fields are kept, not rejected
}

// ENRKey implements enr.Entry.
func (enrEntry) ENRKey() string { return enrKey }

// localForkID is the fork id of the local chain at its current head.
func (c *Communicator) localForkID() forkid.ID {
	genesisID := c.repo.GenesisBlock().Header().ID()
	head := c.repo.BestBlockSummary().Header.Number()
	return forkid.NewID(genesisID, c.forkConfig, head)
}

// enrAttributes are the entries Protocols() advertises, so the record is complete
// before discovery starts.
func (c *Communicator) enrAttributes() []enr.Entry {
	return []enr.Entry{&enrEntry{ForkID: c.localForkID()}}
}

// V5CandidateFilter returns the predicate p2psrv applies to discv5 candidates before
// dialing: the candidate's advertised fork id must pass the same EIP-2124 filter the
// Status handshake uses. Not an equality check against our own record — a syncing node
// is behind, yet must still accept up-to-date peers.
func (c *Communicator) V5CandidateFilter() func(*enode.Node) bool {
	return func(n *enode.Node) bool {
		var entry enrEntry
		// read the candidate's own fork id from its record; none or undecodable → not a thor peer
		if err := n.Load(&entry); err != nil {
			return false
		}
		return c.forkFilter(entry.ForkID) == nil
	}
}

// startENRUpdater keeps the "vet" entry of ln in step with the local head. The fork
// id only changes when the head crosses an activation height, so the record is only
// touched then.
func (c *Communicator) startENRUpdater(ln *enode.LocalNode) {
	ticker := c.repo.NewTicker()
	published := c.localForkID()
	ln.Set(&enrEntry{ForkID: published})
	c.goes.Go(func() {
		for {
			select {
			case <-ticker.C():
				if id := c.localForkID(); id != published {
					ln.Set(&enrEntry{ForkID: id})
					published = id
				}
			case <-c.ctx.Done():
				return
			}
		}
	})
}
