// Copyright (c) 2018 The VeChainThor developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package node

import (
	"crypto/ecdsa"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/vechain/thor/thor"
)

// Master represents the master's key.
type Master struct {
	PrivateKey *ecdsa.PrivateKey
	Address    thor.Address
}

// Masters is the list of master.
type Masters []Master

// NewMasters creates masters.
func NewMasters(keys []*ecdsa.PrivateKey) Masters {
	ms := make(Masters, 0, len(keys))

	for _, key := range keys {
		ms = append(ms, Master{
			PrivateKey: key,
			Address:    thor.Address(crypto.PubkeyToAddress(key.PublicKey)),
		})
	}

	return ms
}

// GetPrivateKey gets privatekey by address
func (ms Masters) GetPrivateKey(master thor.Address) *ecdsa.PrivateKey {
	for _, m := range ms {
		if m.Address == master {
			return m.PrivateKey
		}
	}
	return nil
}

// Addresses returns the address list of masters.
func (ms Masters) Addresses() []thor.Address {
	addrs := make([]thor.Address, 0, len(ms))
	for _, m := range ms {
		addrs = append(addrs, m.Address)
	}
	return addrs
}
