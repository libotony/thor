// Copyright (c) 2026 The VeChainThor developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package p2psrv

import (
	"github.com/ethereum/go-ethereum/p2p"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/vechain/thor/v2/p2p/discv5"
)

type testApp struct {
	protos []*p2p.Protocol
	topic  discv5.Topic
	accept func(*enode.Node) bool // nil accepts everything
}

func (a testApp) Protocols() []*p2p.Protocol { return a.protos }
func (a testApp) DiscTopic() discv5.Topic    { return a.topic }
func (a testApp) AcceptV5Candidate(n *enode.Node) bool {
	return a.accept == nil || a.accept(n)
}
