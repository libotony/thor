// Copyright (c) 2020 The VeChainThor developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package node

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/event"
	"github.com/hashicorp/golang-lru/simplelru"
	"github.com/pkg/errors"
	"github.com/vechain/go-ecvrf"
	"github.com/vechain/thor/block"
	"github.com/vechain/thor/builtin"
	"github.com/vechain/thor/cache"
	"github.com/vechain/thor/comm"
	"github.com/vechain/thor/comm/proto"
	"github.com/vechain/thor/poa"
	"github.com/vechain/thor/thor"
)

var (
	seenDeclaration, _ = simplelru.NewLRU(512, nil)
	seenProposer, _    = simplelru.NewLRU(512, nil)
	seenAccepted, _    = simplelru.NewLRU(512, nil)

	knownDeclaration   = cache.NewPrioCache(16)
	unknownDeclaration = cache.NewRandCache(128)
	unknownAccepted    = cache.NewRandCache(128)
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

	newDecCh := make(chan *comm.NewDeclarationEvent)
	scope.Track(n.comm.SubscribeDeclaration(newDecCh))

	newAcceptedCh := make(chan *comm.NewAcceptedEvent)
	scope.Track(n.comm.SubscribeAccepted(newAcceptedCh))

	unknownTicker := time.NewTicker(time.Duration(1) * time.Second)
	defer unknownTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-newDecCh:
			dec := ev.Declaration
			parent := n.repo.BestBlock().Header()

			//  only accept declaration that are within 3 rounds.
			if math.Abs(float64(dec.Number())-float64(parent.Number()+1)) > 3 {
				continue
			}

			// skip if proposal already seen(prevent DoS)
			if seenDeclaration.Contains(dec.Hash()) {
				continue
			}
			seenDeclaration.Add(dec.Hash(), struct{}{})

			if parent.ID() != dec.ParentID {
				unknownDeclaration.Set(ev.Hash(), ev.Declaration)
				continue
			}

			if err := n.validateProposer(dec, parent); err != nil {
				log.Debug("signer not valid", "err", err)
				continue
			}

			if err := validateDeclaration(dec, parent); err != nil {
				log.Debug("block declaration is not valid", "err", err)
				continue
			}

			n.comm.BroadcastDeclaration(dec)
			knownDeclaration.Set(dec.Hash(), dec, float64(dec.Timestamp))

			if isAuthority, err := n.isAuthority(parent, n.master.Address()); err != nil {
				log.Debug("failed to validate master", "err", err)
				continue
			} else if isAuthority == true {
				accepted, err := n.tryBacking(dec, parent)
				if err != nil {
					log.Debug("failed to back an declaration", "err", err)
					continue
				}

				if accepted == nil {
					continue
				}

				seenAccepted.Add(accepted.Hash(), struct{}{})
				n.comm.BroadcastAccepted(accepted)
			}
		case ev := <-newAcceptedCh:
			parent := n.repo.BestBlock().Header()

			// skip if backer signature already seen(prevent DOS)
			if seenAccepted.Contains(ev.Hash()) {
				continue
			}
			seenAccepted.Add(ev.Hash(), struct{}{})

			if val, _, ok := knownDeclaration.Get(ev.DeclarationHash); ok == true {
				dec := val.(*block.Declaration)

				if parent.ID() != dec.ParentID {
					continue
				}

				if err := n.validateBackerSignature(ev.Signature, dec, parent); err != nil {
					log.Debug("failed to validate backer signature", "err", err)
					continue
				}

				n.comm.BroadcastAccepted(ev.Accepted)
			} else {
				unknownAccepted.Set(ev.Hash(), ev.Accepted)
			}
		case <-unknownTicker.C:
			parent := n.repo.BestBlock().Header()

			var decs []*block.Declaration
			unknownDeclaration.ForEach(func(ent *cache.Entry) bool {
				decs = append(decs, ent.Value.(*block.Declaration))
				return true
			})
			for _, dec := range decs {
				// remove obsolete declarations
				if math.Abs(float64(dec.Number())-float64(parent.Number()+1)) > 3 {
					unknownDeclaration.Remove(dec.Hash())
				}
				if dec.ParentID == parent.ID() {
					unknownDeclaration.Remove(dec.Hash())

					if err := n.validateProposer(dec, parent); err != nil {
						log.Debug("signer not valid", "err", err)
						continue
					}

					if err := validateDeclaration(dec, parent); err != nil {
						log.Debug("block declaration is not valid", "err", err)
						continue
					}

					n.comm.BroadcastDeclaration(dec)
					knownDeclaration.Set(dec.Hash(), dec, float64(dec.Timestamp))

					if isAuthority, err := n.isAuthority(parent, n.master.Address()); err != nil {
						log.Debug("failed to validate master", "err", err)
						continue
					} else if isAuthority == true {
						accepted, err := n.tryBacking(dec, parent)
						if err != nil {
							log.Debug("failed to back an declaration", "err", err)
							continue
						}

						if accepted == nil {
							continue
						}

						seenAccepted.Add(accepted.Hash(), struct{}{})
						n.comm.BroadcastAccepted(accepted)
					}
				}
			}

			var aps []*proto.Accepted
			unknownAccepted.ForEach(func(ent *cache.Entry) bool {
				aps = append(aps, ent.Value.(*proto.Accepted))
				return true
			})

			for _, ap := range aps {
				if val, _, ok := knownDeclaration.Get(ap.DeclarationHash); ok == true {
					unknownAccepted.Remove(ap.Hash())

					dec := val.(*block.Declaration)

					if parent.ID() != dec.ParentID {
						continue
					}

					if err := n.validateBackerSignature(ap.Signature, dec, parent); err != nil {
						log.Debug("failed to validate backer signature", "err", err)
						continue
					}

					n.comm.BroadcastAccepted(ap)
				}
			}
		}
	}
}

func (n *Node) isAuthority(parent *block.Header, addr thor.Address) (bool, error) {
	st := n.stater.NewState(parent.StateRoot())
	authority := builtin.Authority.Native(st)

	listed, _, _, _, err := authority.Get(addr)
	if err != nil {
		return false, err
	}

	return listed, nil
}

func (n *Node) validateProposer(dec *block.Declaration, parent *block.Header) error {
	signer, err := dec.Signer()
	if err != nil {
		return err
	}
	if isAuthority, err := n.isAuthority(parent, signer); err != nil {
		return err
	} else if isAuthority == false {
		return errors.Errorf("%v is not an authority", signer)
	}

	var key [32]byte
	copy(key[:], signer.Bytes())
	binary.BigEndian.PutUint32(key[20:], dec.Number())
	binary.BigEndian.PutUint64(key[24:], dec.Timestamp)
	if seenProposer.Contains(key) {
		return errors.Errorf("proposer:%v already declared in this round", signer)
	}
	seenProposer.Add(key, struct{}{})

	return nil
}

func (n *Node) tryBacking(dec *block.Declaration, parent *block.Header) (*proto.Accepted, error) {
	seed, err := n.seeder.Generate(parent.ID())
	if err != nil {
		return nil, err
	}

	var num [4]byte
	binary.BigEndian.PutUint32(num[:], parent.Number())
	alpha := thor.Blake2b(seed, num[:])

	beta, proof, err := ecvrf.NewSecp256k1Sha256Tai().Prove(n.master.PrivateKey, alpha.Bytes())
	if err != nil {
		return nil, err
	}

	if lucky := poa.EvaluateVRF(beta); lucky == false {
		return nil, errors.New("not lucky enough")
	}

	proposer, _ := dec.Signer()
	msg := dec.AsMessage(proposer)

	input := make([]byte, 0, 113)
	input = append(input, msg.Bytes()...)
	input = append(input, proof...)
	signature, err := crypto.Sign(thor.Blake2b(input).Bytes(), n.master.PrivateKey)
	if err != nil {
		return nil, err
	}

	bs, err := block.NewComplexSignature(proof, signature)
	if err != nil {
		return nil, err
	}

	accepted := proto.Accepted{
		DeclarationHash: dec.Hash(),
		Signature:       bs,
	}
	return &accepted, nil
}

func (n *Node) validateBackerSignature(bs *block.ComplexSignature, dec *block.Declaration, parent *block.Header) error {
	proposer, _ := dec.Signer()
	msg := dec.AsMessage(proposer)

	backer, pub, err := bs.Signer(msg)
	if err != nil {
		return nil
	}

	if isAuthority, err := n.isAuthority(parent, backer); err != nil {
		return err
	} else if isAuthority == false {
		return errors.Errorf("backer:%v is not an authority", backer)
	}

	seed, err := n.seeder.Generate(parent.ID())
	if err != nil {
		return err
	}

	var num [4]byte
	binary.BigEndian.PutUint32(num[:], parent.Number())
	alpha := thor.Blake2b(seed, num[:])

	beta, err := bs.Validate(pub, alpha)
	if err != nil {
		return err
	}

	if isBacker := poa.EvaluateVRF(beta); isBacker == false {
		return fmt.Errorf("VRF output is not lucky enough to be a backer: %v", crypto.PubkeyToAddress(*pub))
	}
	return nil
}

func validateDeclaration(dec *block.Declaration, parent *block.Header) error {
	now := uint64(time.Now().Unix())
	if dec.Timestamp <= parent.Timestamp() {
		return errors.New("proposal timestamp behind parents")
	}

	if (dec.Timestamp-parent.Timestamp())%thor.BlockInterval != 0 {
		return errors.New("block interval not rounded")
	}

	if dec.Timestamp > now+thor.BlockInterval {
		return errors.New("proposal in the future")
	}
	return nil
}
