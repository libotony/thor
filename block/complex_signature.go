// Copyright (c) 2020 The VeChainThor developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package block

import (
	"crypto/ecdsa"
	"errors"
	"io"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/vechain/go-ecvrf"
	"github.com/vechain/thor/thor"
	"github.com/vechain/thor/trie"
)

var (
	emptyRoot = trie.DeriveRoot(&derivableComplexSignatures{})
)

// ComplexSignature is the signature from backer.
// Composed by [ VRF Proof(81 bytes) + Secp256k1 Signature(65 bytes)]
type ComplexSignature struct {
	body  []byte
	alpha thor.Bytes32
}

// NewComplexSignature creates a new signature.
func NewComplexSignature(proof, signature []byte) (*ComplexSignature, error) {
	if len(proof) != 81 {
		return nil, errors.New("invalid proof length, 81 bytes required")
	}
	if len(signature) != 65 {
		return nil, errors.New("invalid signature length, 65 bytes required")
	}

	var ms ComplexSignature
	ms.body = make([]byte, 0, 146)
	ms.body = append(ms.body, proof...)
	ms.body = append(ms.body, signature...)

	return &ms, nil
}

// Bytes returns the content in byte slice.
func (ms *ComplexSignature) Bytes() []byte {
	return append([]byte(nil), ms.body...)
}

// Signer returns the signer of Secp256k1 signature.
// Complex signature does contain message(declaration), we need to compute the msg hash first.
func (ms *ComplexSignature) Signer(msg thor.Bytes32) (signer thor.Address, pub *ecdsa.PublicKey, err error) {
	input := make([]byte, 0, 113)
	input = append(input, msg.Bytes()...)
	input = append(input, ms.body[:81]...)

	signature := make([]byte, 65)
	copy(signature[:], ms.body[81:])

	if pub, err = crypto.SigToPub(thor.Blake2b(input).Bytes(), signature); err != nil {
		return
	}

	signer = thor.Address(crypto.PubkeyToAddress(*pub))
	return
}

// Validate validates the VRF proof, returns the beta.
func (ms *ComplexSignature) Validate(pub *ecdsa.PublicKey, alpha thor.Bytes32) (beta []byte, err error) {
	proof := make([]byte, 81)
	copy(proof[:], ms.body[:])

	beta, err = ecvrf.NewSecp256k1Sha256Tai().Verify(pub, alpha.Bytes(), proof)
	return
}

// EncodeRLP implements rlp.Encoder.
func (ms *ComplexSignature) EncodeRLP(w io.Writer) error {
	return rlp.Encode(w, &ms.body)
}

// DecodeRLP implements rlp.Decoder.
func (ms *ComplexSignature) DecodeRLP(s *rlp.Stream) error {
	var body []byte

	if err := s.Decode(&body); err != nil {
		return err
	}
	*ms = ComplexSignature{body: body}
	return nil
}

// ComplexSignatures is the list of VRF signature.
type ComplexSignatures []*ComplexSignature

// RootHash computes merkle root hash of ComplexSignatures.
func (mss ComplexSignatures) RootHash() thor.Bytes32 {
	if len(mss) == 0 {
		// optimized
		return emptyRoot
	}
	return trie.DeriveRoot(derivableComplexSignatures(mss))
}

// implements DerivableList.
type derivableComplexSignatures ComplexSignatures

func (d derivableComplexSignatures) Len() int {
	return len(d)
}
func (d derivableComplexSignatures) GetRlp(i int) []byte {
	data, err := rlp.EncodeToBytes(d[i])
	if err != nil {
		panic(err)
	}
	return data
}
