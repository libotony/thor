// Copyright (c) 2026 The VeChainThor developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package p2psrv

// V5ProtocolID replaces the upstream "discv5" packet header id, isolating the
// VeChain DHT from Ethereum's at the wire level: a mismatched id fails v5wire
// header validation and the packet is dropped before any handshake.
var V5ProtocolID = [6]byte{'v', 'e', 'c', 'h', 'n', '5'}
