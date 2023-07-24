// Copyright (c) 2019 The VeChainThor developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/pkg/errors"
	"github.com/vechain/thor/block"
	"github.com/vechain/thor/chain"
	"github.com/vechain/thor/co"
	"github.com/vechain/thor/consensus"
	"github.com/vechain/thor/state"
	"github.com/vechain/thor/thor"
	"github.com/vechain/thor/tracers"
	"github.com/vechain/thor/tracers/logger"
	"github.com/vechain/thor/vm"
	"gopkg.in/cheggaaa/pb.v1"
)

type callTracerConfig struct {
	OnlyTopCall bool `json:"onlyTopCall"` // If true, call tracer won't collect any subcalls
	WithLog     bool `json:"withLog"`     // If true, call tracer will collect event logs
}

type prestateTracerConfig struct {
	DiffMode bool `json:"diffMode"` // If true, this tracer will return state modifications
}

func verifyTracer(ctx context.Context, repo *chain.Repository, stater *state.Stater, forkConfig thor.ForkConfig, startPos uint32) error {
	best := repo.BestBlockSummary()
	bestNum := best.Header.Number()
	if bestNum == startPos {
		return nil
	}

	fmt.Println(">> Verifying tracer <<")
	if startPos == 0 {
		startPos = 1 // block 0 can be skipped
	}
	pb := pb.New64(int64(bestNum)).
		Set64(int64(startPos - 1)).
		SetMaxWidth(90).
		Start()

	defer func() { pb.NotPrint = true }()

	var (
		goes    co.Goes
		pumpErr error
		ch      = make(chan *block.Block, 1000)
		cancel  func()
	)

	ctx, cancel = context.WithCancel(ctx)
	defer goes.Wait()
	goes.Go(func() {
		defer close(ch)
		pumpErr = pumpBlocks(ctx, repo, best.Header.ID(), startPos, bestNum, ch)
	})

	defer cancel()

	topCallConfig, _ := json.Marshal(callTracerConfig{OnlyTopCall: true})
	withLogConfig, _ := json.Marshal(callTracerConfig{WithLog: true})
	diffConfig, _ := json.Marshal(prestateTracerConfig{DiffMode: true})

	cons := consensus.New(repo, stater, forkConfig)
	var runErr error
	<-co.Parallel(func(queue chan<- func()) {
		for b := range ch {
			b := b

			if runErr != nil {
				break
			}
			if len(b.Transactions()) > 0 {
				queue <- func() {
					if err := runTracer(b, cons, "", nil); err != nil {
						runErr = err
					}
				}
				queue <- func() {
					if err := runTracer(b, cons, "callTracer", topCallConfig); err != nil {
						runErr = err
					}
				}
				queue <- func() {
					if err := runTracer(b, cons, "callTracer", withLogConfig); err != nil {
						runErr = err
					}
				}
				queue <- func() {
					if err := runTracer(b, cons, "prestateTracer", nil); err != nil {
						runErr = err
					}
				}
				queue <- func() {
					if err := runTracer(b, cons, "prestateTracer", diffConfig); err != nil {
						runErr = err
					}
				}
				queue <- func() {
					if err := runTracer(b, cons, "4byteTracer", nil); err != nil {
						runErr = err
					}
				}
			}
			pb.Add(1)
		}
	})

	pb.Finish()

	if runErr != nil {
		return runErr
	}
	return pumpErr
}

func runTracer(b *block.Block, cons *consensus.Consensus, name string, config json.RawMessage) error {
	rt, err := cons.NewRuntimeForReplay(b.Header(), false)
	if err != nil {
		return err
	}

	for txIndex, tx := range b.Transactions() {
		txExec, err := rt.PrepareTransaction(tx)
		if err != nil {
			return errors.Wrap(err, b.Header().ID().String()+": prepare transaction")
		}

		clauseCounter := 0
		for txExec.HasNextClause() {
			var tracer tracers.Tracer
			if name == "" {
				tracer, _ = logger.NewStructLogger(config)
			} else {
				tracer, _ = tracers.DefaultDirectory.New(name, config)
			}
			tracer.SetContext(&tracers.Context{
				BlockID:     b.Header().ID(),
				BlockTime:   b.Header().Timestamp(),
				TxID:        tx.ID(),
				TxIndex:     txIndex,
				ClauseIndex: clauseCounter,
				State:       rt.State(),
			})
			rt.SetVMConfig(vm.Config{Tracer: tracer, Debug: true})

			_, _, err := txExec.NextClause()
			if err != nil {
				return errors.Wrap(err, b.Header().ID().String()+": next clause")
			}
			clauseCounter++
			_, err = tracer.GetResult()
			if err != nil {
				return errors.Wrap(err, b.Header().ID().String()+": get tracer result")
			}
		}
		if _, err := txExec.Finalize(); err != nil {
			return errors.Wrap(err, b.Header().ID().String()+": finalize transaction")
		}
	}
	return nil
}

func pumpBlocks(ctx context.Context, repo *chain.Repository, headID thor.Bytes32, from, to uint32, ch chan<- *block.Block) error {
	var (
		chain = repo.NewChain(headID)
		buf   []*block.Block
	)
	const bufLen = 256
	for i := from; i <= to; i++ {
		b, err := chain.GetBlock(i)
		if err != nil {
			return err
		}

		buf = append(buf, b)
		if len(buf) >= bufLen {
			select {
			case <-co.Parallel(func(queue chan<- func()) {
				for _, b := range buf {
					h := b.Header()
					queue <- func() {
						h.ID()
					}
					for _, tx := range b.Transactions() {
						tx := tx
						queue <- func() {
							tx.ID()
						}
					}
				}
			}):
			case <-ctx.Done():
				return ctx.Err()
			}

			for _, b := range buf {
				select {
				case ch <- b:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			buf = buf[:0]
		}
		// recreate the chain to avoid the internal trie holds too many nodes.
		if n := i - from; n > 0 && n%10000 == 0 {
			chain = repo.NewChain(headID)
		}
	}

	// pump remained blocks
	for _, b := range buf {
		select {
		case ch <- b:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}
