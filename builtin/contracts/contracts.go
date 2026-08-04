// Copyright (c) 2018 The VeChainThor developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

// Package contracts holds dependency-free metadata (address and ABI) for
// builtin contracts, extracted so it can be imported without pulling in
// state/chain/muxdb.
package contracts

import (
	"github.com/pkg/errors"

	"github.com/vechain/thor/v2/abi"
	"github.com/vechain/thor/v2/builtin/gen"
	"github.com/vechain/thor/v2/thor"
)

type Contract struct {
	name    string
	Address thor.Address
	ABI     *abi.ABI
}

func mustLoad(name string) *Contract {
	asset := "compiled/" + name + ".abi"
	data := gen.MustABI(asset)
	abi, err := abi.New(data)
	if err != nil {
		panic(errors.Wrap(err, "load ABI for '"+name+"'"))
	}

	return &Contract{
		name,
		thor.BytesToAddress([]byte(name)),
		abi,
	}
}

// RuntimeBytecodes load runtime byte codes.
func (c *Contract) RuntimeBytecodes() []byte {
	asset := "compiled/" + c.name + ".bin-runtime"
	data := gen.MustBIN(asset)
	return data
}

// RawABI load raw ABI data.
func (c *Contract) RawABI() []byte {
	asset := "compiled/" + c.name + ".abi"
	data := gen.MustABI(asset)
	return data
}

func (c *Contract) NativeABI() *abi.ABI {
	asset := "compiled/" + c.name + "Native.abi"
	data := gen.MustABI(asset)
	abi, err := abi.New(data)
	if err != nil {
		panic(errors.Wrap(err, "load native ABI for '"+c.name+"'"))
	}
	return abi
}

// Builtin contracts metadata.
var (
	Params      = mustLoad("Params")
	Authority   = mustLoad("Authority")
	Energy      = mustLoad("Energy")
	Executor    = mustLoad("Executor")
	Prototype   = mustLoad("Prototype")
	Extension   = mustLoad("Extension")
	ExtensionV2 = mustLoad("ExtensionV2")
	ExtensionV3 = mustLoad("ExtensionV3")
	Staker      = mustLoad("Staker")
	Measure     = mustLoad("Measure")
)
