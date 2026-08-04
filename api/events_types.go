// Copyright (c) 2018 The VeChainThor developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package api

import "github.com/vechain/thor/v2/api/dto"

type (
	FilteredEvent = dto.FilteredEvent
	TopicSet      = dto.TopicSet
	EventCriteria = dto.EventCriteria
	Options       = dto.Options
	EventFilter   = dto.EventFilter
	RangeType     = dto.RangeType
	Range         = dto.Range
)

const (
	BlockRangeType = dto.BlockRangeType
	TimeRangeType  = dto.TimeRangeType
)
