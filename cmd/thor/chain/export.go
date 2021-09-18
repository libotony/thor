// Copyright (c) 2021 The VeChainThor developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package chain

import (
	"compress/gzip"
	"context"
	"os"

	"github.com/ethereum/go-ethereum/rlp"
	"github.com/vechain/thor/chain"
	"gopkg.in/cheggaaa/pb.v1"
)

func ExportChain(ctx context.Context, repo *chain.Repository, fd *os.File) error {
	writer := gzip.NewWriter(fd)
	defer writer.Close()

	chain := repo.NewBestChain()
	bestNum := repo.BestBlock().Header().Number()

	if err := rlp.Encode(writer, repo.GenesisBlock()); err != nil {
		return err
	}

	pb := pb.New64(int64(bestNum)).
		Set64(int64(0)).
		SetMaxWidth(90).
		Start()

	pos := uint32(1)
	for {
		if pos > bestNum {
			break
		}

		b, err := chain.GetBlock(pos)
		if err != nil {
			return err
		}

		if err := rlp.Encode(writer, b); err != nil {
			return err
		}

		pos++
		pb.Add(1)

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}
	pb.Finish()

	return nil
}
