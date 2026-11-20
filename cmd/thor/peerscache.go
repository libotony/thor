// Copyright (c) 2026 The VeChainThor developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package main

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/ethereum/go-ethereum/p2p/enode"
)

// peersCacheFile holds the warm-start peer list (enode URLs). Not the legacy
// peers.cache, whose RLP layout is tied to the removed discover.Node type.
const peersCacheFile = "peers-v2.cache"

// loadPeersCache loads the warm-start peers saved under instanceDir by a previous run.
func loadPeersCache(instanceDir string) []*enode.Node {
	return loadNodesCache(filepath.Join(instanceDir, peersCacheFile))
}

// savePeersCache writes the warm-start peers under instanceDir for the next run.
func savePeersCache(instanceDir string, nodes []*enode.Node) error {
	return saveNodesCache(filepath.Join(instanceDir, peersCacheFile), nodes)
}

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
	return os.WriteFile(path, data, 0o600)
}
