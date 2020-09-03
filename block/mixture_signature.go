// Copyright (c) 2020 The VeChainThor developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package block

import (
	"crypto/ecdsa"
	"errors"
	"io"
	"sync/atomic"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/vechain/go-ecvrf"
	"github.com/vechain/thor/thor"
	"github.com/vechain/thor/trie"
)

var (
	emptyRoot = trie.DeriveRoot(&derivableMixtureSignatures{})
)

// MixtureSignature is the mixed signature from backer.
// Composed by [ VRF Proof(81 bytes) + Secp256k1 Signature(65 bytes)]
type MixtureSignature struct {
	body  []byte
	alpha thor.Bytes32
	cache struct {
		beta atomic.Value
		pub  atomic.Value
	}
}

// NewMixtureSignature creates a new signature.
func NewMixtureSignature(proof, signature []byte) (*MixtureSignature, error) {
	if len(proof) != 81 {
		return nil, errors.New("invalid proof length, 81 bytes required")
	}
	if len(signature) != 65 {
		return nil, errors.New("invalid signature length, 65 bytes required")
	}

	var ms MixtureSignature
	ms.body = append(ms.body, proof...)
	ms.body = append(ms.body, signature...)

	return &ms, nil
}

// Bytes returns the content in byte slice.
func (ms *MixtureSignature) Bytes() []byte {
	return append([]byte(nil), ms.body...)
}

// WithAlpha creates a new mixture signature with alpha set.
func (ms *MixtureSignature) WithAlpha(alpha thor.Bytes32) *MixtureSignature {
	cpy := MixtureSignature{body: ms.body, alpha: alpha}
	return &cpy
}

// Signer returns the signer of Secp256k1 signature.
func (ms *MixtureSignature) Signer() (signer thor.Address, err error) {
	if ms.alpha.IsZero() {
		err = errors.New("invalid alpha")
		return
	}
	if cached := ms.cache.pub.Load(); cached != nil {
		signer = thor.Address(crypto.PubkeyToAddress(cached.(ecdsa.PublicKey)))
		return
	}

	var pub ecdsa.PublicKey
	defer func() {
		if err == nil {
			ms.cache.pub.Store(pub)
		}
	}()

	msg := make([]byte, 32+81)
	signature := make([]byte, 65)
	copy(msg[:], ms.alpha.Bytes())
	copy(msg[32:], ms.body[:])
	copy(signature[:], ms.body[81:])

	key, err := crypto.SigToPub(thor.Blake2b(msg).Bytes(), signature)
	if err != nil {
		return
	}

	pub = *key
	signer = thor.Address(crypto.PubkeyToAddress(pub))
	return
}

// Validate validates the VRF proof, returns the beta.
func (ms *MixtureSignature) Validate() (beta []byte, err error) {
	_, err = ms.Signer()
	if err != nil {
		return
	}
	if cached := ms.cache.beta.Load(); cached != nil {
		return cached.([]byte), nil
	}
	defer func() {
		if err == nil {
			ms.cache.beta.Store(beta)
		}
	}()

	pub := ms.cache.pub.Load().(ecdsa.PublicKey)
	proof := make([]byte, 81)
	copy(proof[:], ms.body[:])

	beta, err = ecvrf.NewSecp256k1Sha256Tai().Verify(&pub, ms.alpha.Bytes(), proof)
	return
}

// EncodeRLP implements rlp.Encoder.
func (ms *MixtureSignature) EncodeRLP(w io.Writer) error {
	return rlp.Encode(w, &ms.body)
}

// DecodeRLP implements rlp.Decoder.
func (ms *MixtureSignature) DecodeRLP(s *rlp.Stream) error {
	var body []byte

	if err := s.Decode(&body); err != nil {
		return err
	}
	*ms = MixtureSignature{body: body}
	return nil
}

// MixtureSignatures is the list of VRF signature.
type MixtureSignatures []*MixtureSignature

// RootHash computes merkle root hash of MixtureSignatures.
func (mss MixtureSignatures) RootHash() thor.Bytes32 {
	if len(mss) == 0 {
		// optimized
		return emptyRoot
	}
	return trie.DeriveRoot(derivableMixtureSignatures(mss))
}

// implements DerivableList.
type derivableMixtureSignatures MixtureSignatures

func (d derivableMixtureSignatures) Len() int {
	return len(d)
}
func (d derivableMixtureSignatures) GetRlp(i int) []byte {
	data, err := rlp.EncodeToBytes(d[i])
	if err != nil {
		panic(err)
	}
	return data
}
