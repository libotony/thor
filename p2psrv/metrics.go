// Copyright (c) 2024 The VeChainThor developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package p2psrv

import "github.com/vechain/thor/v2/metrics"

var (
	metricConnectedPeers = metrics.LazyLoadGauge("p2p_connected_peers_gauge")
	// Nodes handed to the dial scheduler, by discovery stack ("legacy" or "v5").
	metricDialCandidates = metrics.LazyLoadCounterVec("p2p_dial_candidates_counter", []string{"source"})
	// Connected peers that the modern discv5 stack can find; divide by p2p_connected_peers_gauge.
	metricV5DiscoverablePeers = metrics.LazyLoadGauge("p2p_v5_discoverable_peers_gauge")
)
