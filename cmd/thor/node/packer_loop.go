// Copyright (c) 2018 The VeChainThor developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package node

import (
	"context"
	"fmt"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/pkg/errors"
	"github.com/vechain/go-ecvrf"
	"github.com/vechain/thor/block"
	"github.com/vechain/thor/comm"
	"github.com/vechain/thor/packer"
	"github.com/vechain/thor/poa"
	"github.com/vechain/thor/thor"
	"github.com/vechain/thor/tx"
)

func (n *Node) packerLoop(ctx context.Context) {
	log.Debug("enter packer loop")
	defer log.Debug("leave packer loop")

	log.Info("waiting for synchronization...")
	select {
	case <-ctx.Done():
		return
	case <-n.comm.Synced():
	}
	log.Info("synchronization process done")

	var (
		authorized bool
		ticker     = n.repo.NewTicker()
	)

	n.packer.SetTargetGasLimit(n.targetGasLimit)

	for {
		now := uint64(time.Now().Unix())

		if n.targetGasLimit == 0 {
			// no preset, use suggested
			suggested := n.bandwidth.SuggestGasLimit()
			n.packer.SetTargetGasLimit(suggested)
		}

		flow, err := n.packer.Schedule(n.repo.BestBlock(), now)
		if err != nil {
			if authorized {
				authorized = false
				log.Warn("unable to pack block", "err", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C():
				continue
			}
		}

		if !authorized {
			authorized = true
			log.Info("prepared to pack block")
		}
		log.Debug("scheduled to pack block", "after", time.Duration(flow.When()-now)*time.Second)

		for {
			if n.timeToPack(flow) == true {
				if err := n.pack(flow); err != nil {
					log.Error("failed to pack block", "err", err)
				}
				break
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
				if n.needReSchedule(flow) {
					log.Debug("re-schedule packer due to new best block")
					goto RE_SCHEDULE
				}
			}
		}
	RE_SCHEDULE:
	}
}

func (n *Node) needReSchedule(flow *packer.Flow) bool {
	best := n.repo.BestBlock().Header()

	if flow.Number() < n.forkConfig.VIP193 {
		/* Before VIP193, re-schedule regarding the following two conditions:
		1. a new block with better total score replaced parent block becomes the best block(at same block height).
		2. new best block has a higher total score.
		*/
		if (best.Number() == flow.ParentHeader().Number() && best.TotalScore() != flow.ParentHeader().TotalScore()) ||
			n.repo.BestBlock().Header().TotalScore() > flow.TotalScore() {
			return true
		}
	}

	/* After VIP-193, re-schedule regarding the following two conditions:
	1. new best block at a different block height.
	2. new blest block at the same block height but with a different total score.
	*/
	if (best.Number() == flow.ParentHeader().Number() && best.TotalScore() != flow.ParentHeader().TotalScore()) ||
		best.Number() != flow.ParentHeader().Number() {
		return true
	}

	return false
}

func (n *Node) timeToPack(flow *packer.Flow) bool {
	nowTs := uint64(time.Now().Unix())
	// start immediately in post vip 193 stage, to allow more time for getting backer signature
	if flow.ParentHeader().Number() >= n.forkConfig.VIP193 {
		return nowTs+thor.BlockInterval >= flow.When()
	}
	// blockInterval/2 early to allow more time for processing txs
	return nowTs+thor.BlockInterval/2 >= flow.When()
}

func (n *Node) pack(flow *packer.Flow) error {
	txs := n.txPool.Executables()
	var txsToRemove []*tx.Transaction
	defer func() {
		for _, tx := range txsToRemove {
			n.txPool.Remove(tx.Hash(), tx.ID())
		}
	}()

	var scope event.SubscriptionScope
	defer scope.Close()

	for _, tx := range txs {
		if err := flow.Adopt(tx); err != nil {
			if packer.IsGasLimitReached(err) {
				break
			}
			if packer.IsTxNotAdoptableNow(err) {
				continue
			}
			txsToRemove = append(txsToRemove, tx)
		}
	}

	if flow.Number() >= n.forkConfig.VIP193 {
		proposal, err := flow.Propose(n.master.PrivateKey)
		if err != nil {
			return nil
		}
		n.comm.BroadcastProposal(proposal)

		now := uint64(time.Now().Unix())
		if now < flow.When()-1 {
			newAccCh := make(chan *comm.NewAcceptedEvent)
			scope.Track(n.comm.SubscribeAccepted(newAccCh))

			ticker := time.NewTimer(time.Duration(flow.When()-1-now) * time.Second)
			defer ticker.Stop()

			msg := proposal.AsMessage(n.master.Address())
			alpha := append([]byte(nil), flow.Seed()...)
			alpha = append(alpha, flow.ParentHeader().ID().Bytes()[:4]...)

			b, _ := rlp.EncodeToBytes(proposal)
			hash := thor.Blake2b(b)
			for {
				select {
				case ev := <-newAccCh:
					if flow.Number() >= n.forkConfig.VIP193 {
						if ev.ProposalHash == hash {
							if validateBackerSignature(ev.Signature, flow, msg, alpha); err != nil {
								log.Debug("failed to process backer signature", "err", err)
								continue
							}
						}
					}
				case <-ticker.C:
					goto NEXT
				}
			}
		NEXT:
		}
	}

	newBlock, stage, receipts, err := flow.Pack(n.master.PrivateKey)
	if err != nil {
		return err
	}

	prevTrunk, curTrunk, err := n.commitBlock(stage, newBlock, receipts)
	if err != nil {
		return errors.WithMessage(err, "commit block")
	}

	n.processFork(prevTrunk, curTrunk)

	if prevTrunk.HeadID() != curTrunk.HeadID() {
		n.comm.BroadcastBlock(newBlock)
		log.Info("📦 new block packed",
			"txs", len(receipts),
			"mgas", float64(newBlock.Header().GasUsed())/1000/1000,
			"id", shortID(newBlock.Header().ID()),
		)
	}

	return nil
}

func validateBackerSignature(sig block.ComplexSignature, flow *packer.Flow, msg []byte, alpha []byte) (err error) {
	pub, err := crypto.SigToPub(thor.Blake2b(msg, sig.Proof()).Bytes(), sig.Signature())
	if err != nil {
		return
	}
	backer := thor.Address(crypto.PubkeyToAddress(*pub))

	if flow.IsBackerKnown(backer) == true {
		return errors.New("known backer")
	}

	if flow.GetAuthority(backer) == nil {
		return fmt.Errorf("backer: %v is not an authority", backer)
	}

	beta, err := ecvrf.NewSecp256k1Sha256Tai().Verify(pub, alpha, sig.Proof())
	if err != nil {
		return
	}
	if poa.EvaluateVRF(beta) == true {
		flow.AddBackerSignature(sig, beta, backer)
	} else {
		return fmt.Errorf("invalid proof from %v", backer)
	}
	return
}
