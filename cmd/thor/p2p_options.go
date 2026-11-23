// Copyright (c) 2024 The VeChainThor developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package main

import (
	"crypto/ecdsa"
	"fmt"
	"runtime"
	"slices"

	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/p2p/nat"
	"github.com/pkg/errors"

	"github.com/vechain/thor/v2/p2psrv"
	"github.com/vechain/thor/v2/thor"
)

// p2pInputs is everything buildP2POptions needs, already parsed.
type p2pInputs struct {
	Version        string
	PrivateKey     *ecdsa.PrivateKey
	NAT            nat.Interface
	MaxPeers       int
	ListenAddr     string
	AllowedPeers   []*enode.Node
	StaticPeers    []*enode.Node
	CachedPeers    []*enode.Node
	BootstrapNodes []*enode.Node

	GenesisID              thor.Bytes32
	DisableLegacyDiscovery bool
}

// buildP2POptions assembles the p2p server options. It does no I/O.
func buildP2POptions(in p2pInputs) (*p2psrv.Options, error) {
	allowedPeers, staticPeers, cachedPeers, bootstrapNodes := in.AllowedPeers, in.StaticPeers, in.CachedPeers, in.BootstrapNodes

	for _, n := range slices.Concat(allowedPeers, staticPeers) {
		if n.TCP() == 0 {
			return nil, fmt.Errorf("node %s has no tcp endpoint; --allowed-peers/--static-peers need enode:// URLs or ENRs with a tcp port", n.ID())
		}
	}

	for _, n := range bootstrapNodes {
		if err := n.ValidateComplete(); err != nil {
			return nil, fmt.Errorf("bootnode %s: %w", n, err)
		}
	}

	if len(allowedPeers) > 0 && len(staticPeers) > 0 {
		return nil, errors.New("--allowed-peers and --static-peers are mutually exclusive: allowed-peers is an exclusive whitelist with discovery disabled, static-peers pins peers on top of discovery")
	}

	// Only the official networks have built-in bootnodes and a remote list; any other network
	// is seeded by --bootnode alone.
	opts := &p2psrv.Options{
		Name:              fmt.Sprintf("thor/v%s/%s/%s", in.Version, runtime.GOOS, runtime.Version()),
		PrivateKey:        in.PrivateKey,
		MaxPeers:          in.MaxPeers,
		ListenAddr:        in.ListenAddr,
		NoLegacyDiscovery: in.DisableLegacyDiscovery,
		NAT:               in.NAT,
	}
	if isOfficialNetwork(in.GenesisID) {
		opts.DiscoveryNodes = fallbackDiscoveryNodes
		opts.DiscoveryNodesV5 = fallbackDiscoveryNodes
		opts.RemoteDiscoveryList = remoteDiscoveryNodesList
	}

	// allowed peers flag will only allow p2psrv to connect to the designated peers
	if len(allowedPeers) > 0 {
		opts.NoDiscovery = true // disable discovery
		opts.DiscoveryNodes = nil
		opts.StaticNodes = allowedPeers
	} else {
		// bootstrap nodes flag will overwrite the default discovery nodes and also disable the remote list
		if len(bootstrapNodes) > 0 {
			opts.RemoteDiscoveryList = ""          // disable the remote list
			opts.DiscoveryNodes = bootstrapNodes   // overwrite the default discovery nodes
			opts.DiscoveryNodesV5 = bootstrapNodes // each bootnode seeds both discovery stacks
			// supplied bootstrap nodes can potentially be p2p node, add to the known nodes; udp-only ones are not dial candidates
			opts.KnownNodes = slices.DeleteFunc(slices.Clone(bootstrapNodes), func(n *enode.Node) bool { return n.TCP() == 0 })
		}

		// cached peers will be appended to existing or flag-set bootstrap nodes
		if len(cachedPeers) > 0 {
			opts.KnownNodes = dedupNodeSlice(opts.KnownNodes, cachedPeers)
		}

		// static peers are redialed on disconnect, on top of (not instead of) discovery
		opts.StaticNodes = staticPeers
	}

	return opts, nil
}

func dedupNodeSlice(slice1, slice2 []*enode.Node) []*enode.Node {
	foundMap := map[enode.ID]bool{}
	var dedupedSlice []*enode.Node

	for _, item := range append(slice1, slice2...) {
		if _, ok := foundMap[item.ID()]; ok {
			continue
		}
		foundMap[item.ID()] = true
		dedupedSlice = append(dedupedSlice, item)
	}

	return dedupedSlice
}
