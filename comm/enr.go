// Copyright (c) 2026 The VeChainThor developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package comm

import (
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/rlp"

	"github.com/vechain/thor/v2/forkid"
)

// vechainEntry is the "vet" ENR entry carrying the local fork id.
type vechainEntry struct {
	ForkID forkid.ID
	Rest   []rlp.RawValue `rlp:"tail"` // forward compatibility
}

// ENRKey implements enr.Entry.
func (vechainEntry) ENRKey() string { return "vet" }

func (c *Communicator) currentENREntry() *vechainEntry {
	genesisID := c.repo.GenesisBlock().Header().ID()
	head := c.repo.BestBlockSummary().Header.Number()
	return &vechainEntry{ForkID: forkid.NewID(genesisID, c.forkConfig, head)}
}

// V5CandidateFilter returns a predicate that accepts a discv5 node only when
// its record carries a "vet" entry whose fork id is compatible with ours.
func (c *Communicator) V5CandidateFilter() func(*enode.Node) bool {
	return func(n *enode.Node) bool {
		var entry vechainEntry
		if err := n.Load(&entry); err != nil {
			return false
		}
		return c.forkFilter(entry.ForkID) == nil
	}
}

// StartENRUpdater publishes the current fork id into ln, then refreshes it on
// every best block change until the communicator is stopped.
func (c *Communicator) StartENRUpdater(ln *enode.LocalNode) {
	ticker := c.repo.NewTicker()
	ln.Set(c.currentENREntry())
	c.goes.Go(func() {
		for {
			select {
			case <-ticker.C():
				ln.Set(c.currentENREntry())
			case <-c.ctx.Done():
				return
			}
		}
	})
}
