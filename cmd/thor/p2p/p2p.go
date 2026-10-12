// Copyright (c) 2024 The VeChainThor developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package p2p

import (
	"crypto/ecdsa"
	"fmt"
	"path/filepath"
	"runtime"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/p2p/nat"
	"github.com/pkg/errors"

	"github.com/vechain/thor/v2/comm"
	"github.com/vechain/thor/v2/log"
	"github.com/vechain/thor/v2/p2psrv"
)

// peersCacheFile holds the warm-start peer list (enode URLs). Not the legacy
// peers.cache, whose RLP layout is tied to the removed discover.Node type.
const peersCacheFile = "peers-v2.cache"

type P2P struct {
	comm           *comm.Communicator
	p2pSrv         *p2psrv.Server
	peersCachePath string
	enode          string
}

func New(
	communicator *comm.Communicator,
	privateKey *ecdsa.PrivateKey,
	instanceDir string,
	nat nat.Interface,
	version string,
	maxPeers int,
	listenPort int,
	listenAddr string,
	allowedPeers []*enode.Node,
	cachedPeers []*enode.Node,
	bootstrapNodes []*enode.Node,
) *P2P {
	// known peers will be loaded/stored from/in this file
	peersCachePath := filepath.Join(instanceDir, peersCacheFile)

	// default option setting
	// no known nodes for p2p connection
	// use the hardcoded fallbackDiscoveryNodes for discovery only
	opts := &p2psrv.Options{
		Name:                fmt.Sprintf("thor/v%s/%s/%s", version, runtime.GOOS, runtime.Version()),
		PrivateKey:          privateKey,
		MaxPeers:            maxPeers,
		ListenAddr:          listenAddr,
		DiscoveryNodes:      fallbackDiscoveryNodes,
		RemoteDiscoveryList: remoteDiscoveryNodesList,
		NAT:                 nat,
	}

	// allowed peers flag will only allow p2psrv to connect to the designated peers
	if len(allowedPeers) > 0 {
		opts.NoDiscovery = true // disable discovery
		opts.DiscoveryNodes = nil
		opts.StaticNodes = allowedPeers
	} else {
		// bootstrap nodes flag will overwrite the default discovery nodes and also disable remote discovery
		if len(bootstrapNodes) > 0 {
			opts.RemoteDiscoveryList = ""        // disable remote discovery
			opts.DiscoveryNodes = bootstrapNodes // overwrite the default discovery nodes
			opts.KnownNodes = bootstrapNodes     // supplied bootstrap nodes can potentially be p2p node, add to the known nodes
		}

		// cached peers will be appended to existing or flag-set bootstrap nodes
		if len(cachedPeers) > 0 {
			opts.KnownNodes = dedupNodeSlice(opts.KnownNodes, cachedPeers)
		}
	}

	return &P2P{
		comm:           communicator,
		p2pSrv:         p2psrv.New(opts),
		peersCachePath: peersCachePath,
		enode:          fmt.Sprintf("enode://%x@[extip]:%v", crypto.FromECDSAPub(&privateKey.PublicKey)[1:], listenPort),
	}
}

func (p *P2P) Start() error {
	log.Info("starting P2P networking")
	if err := p.p2pSrv.Start(p.comm.Protocols(), p.comm.DiscTopic()); err != nil {
		return errors.Wrap(err, "start P2P server")
	}
	p.comm.Start()
	return nil
}

func (p *P2P) Stop() {
	log.Info("stopping communicator...")
	p.comm.Stop()

	log.Info("stopping P2P server...")
	p.p2pSrv.Stop()

	log.Info("saving peers cache...")
	if err := saveNodesCache(p.peersCachePath, p.p2pSrv.KnownNodes()); err != nil {
		log.Warn("failed to write peers cache", "err", err)
	}
}

// LoadPeersCache loads the warm-start peers saved under instanceDir by a previous run.
func LoadPeersCache(instanceDir string) []*enode.Node {
	return loadNodesCache(filepath.Join(instanceDir, peersCacheFile))
}

func (p *P2P) Communicator() *comm.Communicator {
	return p.comm
}

func (p *P2P) Enode() string {
	return p.enode
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
