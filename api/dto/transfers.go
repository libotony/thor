// Copyright (c) 2018 The VeChainThor developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package dto

import (
	"github.com/ethereum/go-ethereum/common/math"

	"github.com/vechain/thor/v2/logdb"
	"github.com/vechain/thor/v2/thor"
)

type FilteredTransfer struct {
	Sender    thor.Address          `json:"sender"`
	Recipient thor.Address          `json:"recipient"`
	Amount    *math.HexOrDecimal256 `json:"amount"`
	Meta      LogMeta               `json:"meta"`
}

// TransferFilter.CriteriaSet and Order stay logdb types until Task 17 removes the logdb import here.
type TransferFilter struct {
	CriteriaSet []*logdb.TransferCriteria `json:"criteriaSet,omitempty"`
	Range       *Range                    `json:"range,omitempty"`
	Options     *Options                  `json:"options,omitempty"`
	Order       logdb.Order               `json:"order,omitempty"`
}
