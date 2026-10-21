// Copyright (c) 2026 The VeChainThor developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package p2p

import (
	"encoding/json"
	"os"

	"github.com/ethereum/go-ethereum/p2p/enode"
)

// loadNodesCache reads the peer cache as a JSON list of enode URLv4 strings.
// Any read or decode failure yields nil so the cache is silently rebuilt;
// individual unparsable entries are skipped.
func loadNodesCache(path string) []*enode.Node {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var urls []string
	if err := json.Unmarshal(data, &urls); err != nil {
		return nil
	}
	nodes := make([]*enode.Node, 0, len(urls))
	for _, u := range urls {
		if n, err := enode.ParseV4(u); err == nil {
			nodes = append(nodes, n)
		}
	}
	return nodes
}

func saveNodesCache(path string, nodes []*enode.Node) error {
	urls := make([]string, 0, len(nodes))
	for _, n := range nodes {
		urls = append(urls, n.URLv4())
	}
	data, err := json.Marshal(urls)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}
