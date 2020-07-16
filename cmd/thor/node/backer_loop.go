// Copyright (c) 2020 The VeChainThor developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package node

import (
	"context"
	"fmt"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/event"
	"github.com/hashicorp/golang-lru/simplelru"
	"github.com/vechain/go-ecvrf"
	"github.com/vechain/thor/block"
	"github.com/vechain/thor/cache"
	"github.com/vechain/thor/comm"
	"github.com/vechain/thor/comm/proto"
	"github.com/vechain/thor/poa"
)

func (n *Node) backerLoop(ctx context.Context) {
	log.Debug("enter backer loop")
	defer log.Debug("leave backer loop")

	select {
	case <-ctx.Done():
		return
	case <-n.comm.Synced():
	}

	ticker := n.repo.NewTicker()
	for {
		if n.repo.BestBlock().Header().Number() >= n.forkConfig.VIP193 {
			break
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C():
			continue
		}
	}

	var scope event.SubscriptionScope
	defer scope.Close()

	newProposalCh := make(chan *comm.NewBlockProposalEvent)
	scope.Track(n.comm.SubscribeProposal(newProposalCh))

	newBsCh := make(chan *comm.NewBackerSignatureEvent)
	scope.Track(n.comm.SubscribeBackerSignature(newBsCh))

	unknownTicker := time.NewTicker(time.Duration(1) * time.Second)
	defer unknownTicker.Stop()

	seenProposal, _ := simplelru.NewLRU(100, nil)
	seenBs, _ := simplelru.NewLRU(100, nil)

	var (
		knownProposal = cache.NewPrioCache(10)
		unknownBs     = cache.NewPrioCache(100)
		lastBacked    struct {
			Number uint32
			Score  uint64
		}
	)

	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-newProposalCh:
			best := n.repo.BestBlock()

			proposal := ev.Proposal
			if seenProposal.Contains(proposal.Hash()) == true {
				// skip if proposal already seen
				continue
			} else {
				seenProposal.Add(proposal.Hash(), struct{}{})
			}

			if best.Header().ID() == proposal.ParentID() {
				n.consLock.Lock()
				score, err := n.cons.ValidateProposal(proposal)
				n.consLock.Unlock()
				if err != nil {
					log.Debug("block proposal is not valid", "err", err)
					continue
				}
				knownProposal.Set(proposal.Hash(), proposal, float64(proposal.Timestamp()))
				n.comm.BroadcastProposal(proposal)

				if lastBacked.Number == proposal.Number() && score <= lastBacked.Score {
					log.Debug("already backed, skip this round", "block number", proposal.Number(), "score", score)
					continue
				}
				isAuthority, err := n.isAuthority(best.Header(), n.master.Address())
				if err != nil {
					log.Debug("failed to validate master", "err", err)
					continue
				}
				if isAuthority == true {
					leader, _ := proposal.Signer()
					alpha := proposal.Alpha(leader).Bytes()
					beta, proof, err := ecvrf.NewSecp256k1Sha256Tai().Prove(n.master.PrivateKey, alpha)
					if err != nil {
						log.Debug("failed trying to prove proposal", "err", err)
						continue
					}
					lucky := poa.EvaluateVRF(beta)
					if lucky == false {
						continue
					}

					bs := block.NewBackerSignature(crypto.CompressPubkey(&n.master.PrivateKey.PublicKey), proof)
					full := proto.FullBackerSignature{
						ProposalHash: proposal.Hash(),
						Signature:    bs,
					}
					lastBacked.Number = proposal.Number()
					lastBacked.Score = score

					n.comm.BroadcastBackerSignature(&full)
				}
			}
		case ev := <-newBsCh:
			best := n.repo.BestBlock()

			if seenBs.Contains(ev.Hash()) == true {
				// skip if backer signature already seen
				continue
			} else {
				seenBs.Add(ev.Hash(), struct{}{})
			}

			if err := n.validateBacker(ev.Signature, best.Header()); err != nil {
				log.Debug("failed to verify backer", "err", err)
				continue
			}

			if val, _, ok := knownProposal.Get(ev.FullBackerSignature.ProposalHash); ok == true {
				proposal := val.(*block.Proposal)
				if best.Header().ID() == proposal.ParentID() {
					leader, _ := proposal.Signer()
					alpha := proposal.Alpha(leader)
					if err := n.validateBackerSignature(alpha.Bytes(), ev.Signature); err != nil {
						log.Debug("failed to validate backer signature", "err", err)
						continue
					}

					n.comm.BroadcastBackerSignature(ev.FullBackerSignature)
				}
			} else {
				unknownBs.Set(ev.Hash(), ev.FullBackerSignature, float64(time.Now().Unix()))
			}
		case <-unknownTicker.C:
			var bss []*proto.FullBackerSignature
			unknownBs.ForEach(func(ent *cache.PrioEntry) bool {
				bss = append(bss, ent.Value.(*proto.FullBackerSignature))
				return true
			})
			for _, bs := range bss {
				if val, _, ok := knownProposal.Get(bs.ProposalHash); ok == true {
					unknownBs.Remove(bs.Hash())
					proposal := val.(*block.Proposal)
					best := n.repo.BestBlock()
					if best.Header().ID() == proposal.ParentID() {
						if err := n.validateBacker(bs.Signature, best.Header()); err != nil {
							log.Debug("failed to verify backer", "err", err)
							continue
						}
						leader, _ := proposal.Signer()
						alpha := proposal.Alpha(leader)
						if err := n.validateBackerSignature(alpha.Bytes(), bs.Signature); err != nil {

							log.Debug("failed to validate backer signature", "err", err)
							continue
						}
						n.comm.BroadcastBackerSignature(bs)
					}
				}
			}
		}
	}
}

func (n *Node) validateBacker(bs *block.BackerSignature, parentHeader *block.Header) error {
	signer, err := bs.Signer()
	if err != nil {
		return err
	}
	isAuthority, err := n.isAuthority(parentHeader, signer)
	if err != nil {
		return err
	}
	if isAuthority == false {
		return fmt.Errorf("backer: %v not is not authority", signer)
	}
	return nil
}

func (n *Node) validateBackerSignature(alpha []byte, bs *block.BackerSignature) error {
	signer, _ := bs.Signer()

	beta, err := bs.Validate(alpha)
	if err != nil {
		return err
	}

	isBacker := poa.EvaluateVRF(beta)
	if isBacker == false {
		return fmt.Errorf("signer is not qualified to be a backer: %v", signer)
	}
	return nil
}
