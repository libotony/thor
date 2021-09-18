// Copyright (c) 2021 The VeChainThor developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package chain

import (
	"context"
	"io"

	"github.com/ethereum/go-ethereum/rlp"
	"github.com/pkg/errors"
	"github.com/vechain/thor/block"
	"github.com/vechain/thor/cmd/thor/node"
	"github.com/vechain/thor/co"
)

func ImportChain(parentCtx context.Context, stream *rlp.Stream, n *node.Node) error {
	var (
		handlerErr  error
		goes        co.Goes
		blockStream = make(chan *block.Block, 2048)
	)
	defer goes.Wait()
	defer close(blockStream)

	ctx, cancel := context.WithCancel(parentCtx)
	goes.Go(func() {
		defer cancel()
		handlerErr = n.HandleBlockStream(ctx, blockStream)
	})

	blockNum := 1
	blocks := make([]*block.Block, 1024)
	for {
		blocks = blocks[:0]

		for i := 0; i < 1024; i++ {
			var blk block.Block
			if err := stream.Decode(&blk); err != nil {
				if err == io.EOF {
					return handlerErr
				} else {
					if handlerErr != nil {
						return handlerErr
					}
					return err
				}
			}

			if blk.Header().Number() != uint32(blockNum) {
				return errors.Errorf("broken block sequence, want %d but got %d", blockNum, blk.Header().Number())
			}

			blocks = append(blocks, &blk)
			blockNum++
		}

		select {
		case <-co.Parallel(func(queue chan<- func()) {
			for _, blk := range blocks {
				h := blk.Header()
				queue <- func() { h.ID() }
				for _, tx := range blk.Transactions() {
					tx := tx
					queue <- func() {
						tx.ID()
						tx.UnprovedWork()
						_, _ = tx.IntrinsicGas()
						_, _ = tx.Delegator()
					}
				}
			}
		}):
		case <-ctx.Done():
			if handlerErr != nil {
				return handlerErr
			}
			return ctx.Err()
		}

		for _, blk := range blocks {
			// when queued blocks count > 10% channel cap,
			// send nil block to throttle to reduce mem pressure.
			if len(blockStream)*10 > cap(blockStream) {
				const targetSize = 2048
				for i := 0; i < int(blk.Size())/targetSize-1; i++ {
					select {
					case blockStream <- nil:
					default:
					}
				}
			}
			select {
			case <-ctx.Done():
				if handlerErr != nil {
					return handlerErr
				}
				return ctx.Err()
			case blockStream <- blk:
			}
		}

	}
}
