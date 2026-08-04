// Copyright (c) 2018 The VeChainThor developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package api

import "github.com/vechain/thor/v2/api/dto"

type (
	BlockMessage               = dto.BlockMessage
	TransferMessage            = dto.TransferMessage
	EventMessage               = dto.EventMessage
	SubscriptionEventFilter    = dto.SubscriptionEventFilter
	SubscriptionTransferFilter = dto.SubscriptionTransferFilter
	BeatMessage                = dto.BeatMessage
	Beat2Message               = dto.Beat2Message
	PendingTxIDMessage         = dto.PendingTxIDMessage
)
