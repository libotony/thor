// Copyright (c) 2024 The VeChainThor developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package p2psrv

import "github.com/vechain/thor/v2/metrics"

var (
	metricConnectedPeers = metrics.LazyLoadGauge("p2p_connected_peers_gauge")
	// Connected peers by direction ("inbound" or "outbound").
	metricPeersByDir = metrics.LazyLoadGaugeVec("p2p_connected_peers_by_dir_gauge", []string{"dir"})
	// Nodes handed to the dial scheduler, by source: "legacy" or "v5" discovery, or "known" for nodes seeded from the peers cache and bootnodes.
	metricDialCandidates = metrics.LazyLoadCounterVec("p2p_dial_candidates_counter", []string{"source"})
	// Connected peers that the modern discv5 stack can find; divide by p2p_connected_peers_gauge.
	metricV5DiscoverablePeers = metrics.LazyLoadGauge("p2p_v5_discoverable_peers_gauge")
	// Size of this node's modern discv5 routing table.
	metricV5TableSize = metrics.LazyLoadGauge("p2p_v5_table_size_gauge")
	// Modern discv5 candidates turned down by the application before dialing.
	metricV5CandidatesRejected = metrics.LazyLoadCounter("p2p_v5_candidates_rejected_counter")
)
