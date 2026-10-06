// Copyright 2016 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

// Package internal vendors go-ethereum's params/gas_table.go (removed upstream
// in v1.17). thor's vm retains the table-driven model rather than upstream's
// per-fork JumpTable instructions.
package internal

// GasTable organizes gas prices for different ethereum phases.
type GasTable struct {
	ExtcodeSize uint64
	ExtcodeCopy uint64
	ExtcodeHash uint64
	Balance     uint64
	SLoad       uint64
	Calls       uint64
	Suicide     uint64

	ExpByte uint64

	// CreateBySuicide occurs when the refunded account does not exist. Zero means not charged.
	CreateBySuicide uint64
}

// GasTableHomestead is the Homestead gas table.
var GasTableHomestead = GasTable{
	ExtcodeSize: 20,
	ExtcodeCopy: 20,
	Balance:     20,
	SLoad:       50,
	Calls:       40,
	Suicide:     0,
	ExpByte:     10,
}

// GasTableEIP150 is the EIP-150 (Tangerine Whistle) gas table.
var GasTableEIP150 = GasTable{
	ExtcodeSize: 700,
	ExtcodeCopy: 700,
	Balance:     400,
	SLoad:       200,
	Calls:       700,
	Suicide:     5000,
	ExpByte:     10,

	CreateBySuicide: 25000,
}

// GasTableEIP158 is the EIP-158 (Spurious Dragon) gas table.
var GasTableEIP158 = GasTable{
	ExtcodeSize: 700,
	ExtcodeCopy: 700,
	Balance:     400,
	SLoad:       200,
	Calls:       700,
	Suicide:     5000,
	ExpByte:     50,

	CreateBySuicide: 25000,
}

// GasTableConstantinople is the Constantinople gas table.
var GasTableConstantinople = GasTable{
	ExtcodeSize: 700,
	ExtcodeCopy: 700,
	ExtcodeHash: 400,
	Balance:     400,
	SLoad:       200,
	Calls:       700,
	Suicide:     5000,
	ExpByte:     50,

	CreateBySuicide: 25000,
}
