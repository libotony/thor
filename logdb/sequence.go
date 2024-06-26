// Copyright (c) 2019 The VeChainThor developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package logdb

import "math"

type sequence int64

func newSequence(blockNum uint32, index uint32) sequence {
	if (index & math.MaxInt32) != index {
		panic("index too large")
	}
	return (sequence(blockNum) << 31) | sequence(index)
}

func (s sequence) BlockNumber() uint32 {
	return uint32(s >> 31)
}

func (s sequence) Index() uint32 {
	return uint32(s & math.MaxInt32)
}
