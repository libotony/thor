// Copyright (c) 2026 The VeChainThor developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package tx

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vechain/thor/v2/thor"
)

// newEthDynamicFeeUnsigned returns a zero-signature 0x02 tx ready to be signed.
func newEthDynamicFeeUnsigned(t *testing.T, chainID *big.Int) *Transaction {
	t.Helper()
	to, err := thor.ParseAddress("0x7567d83b7b8d80addcb281a71d54fc7b3364ffed")
	require.NoError(t, err)
	return NewBuilder(TypeEthDynamicFee).
		ChainID(chainID).
		Clause(NewClause(&to).WithValue(big.NewInt(1_000_000)).WithData([]byte{0xAB, 0xCD})).
		MaxFeePerGas(big.NewInt(1_000_000_000_000)).
		MaxPriorityFeePerGas(big.NewInt(1_000_000_000)).
		Gas(21_000).
		Nonce(7).
		Build()
}

// TestEthDynamicFee_BuildFields checks that the Builder produces a tx with the
// expected field values visible through the Transaction API.
func TestEthDynamicFee_BuildFields(t *testing.T) {
	chainID := big.NewInt(100009)
	trx := newEthDynamicFeeUnsigned(t, chainID)

	assert.Equal(t, uint8(TypeEthDynamicFee), trx.Type())
	assert.Equal(t, chainID, trx.ChainID())
	assert.Equal(t, uint64(7), trx.Nonce())
	assert.Equal(t, uint64(21_000), trx.Gas())
	assert.Equal(t, big.NewInt(1_000_000_000_000), trx.MaxFeePerGas())
	assert.Equal(t, big.NewInt(1_000_000_000), trx.MaxPriorityFeePerGas())
	assert.False(t, trx.IsExpired(1_000_000), "0x02 tx must never appear expired")
	assert.Equal(t, byte(0), trx.ChainTag(), "0x02 tx has no ChainTag")
	assert.Equal(t, uint32(0), trx.Expiration(), "0x02 tx has no Expiration")
	assert.Nil(t, trx.DependsOn(), "0x02 tx has no dependsOn")
	// Single synthetic clause carries (to, value, data).
	cls := trx.Clauses()
	require.Len(t, cls, 1)
	assert.Equal(t, big.NewInt(1_000_000), cls[0].Value())
	assert.Equal(t, []byte{0xAB, 0xCD}, cls[0].Data())
}

// TestEthDynamicFee_BuildRejectsBadClauseCount pins the single-clause
// invariant of 0x02 envelopes — Build() must panic on zero or multi-clause
// builders so misuse is caught at construction time, not silently truncated.
func TestEthDynamicFee_BuildRejectsBadClauseCount(t *testing.T) {
	to, _ := thor.ParseAddress("0x7567d83b7b8d80addcb281a71d54fc7b3364ffed")

	// 0 clauses
	assert.Panics(t, func() {
		NewBuilder(TypeEthDynamicFee).
			ChainID(big.NewInt(1)).
			MaxFeePerGas(big.NewInt(1)).MaxPriorityFeePerGas(big.NewInt(1)).
			Gas(21_000).Nonce(0).Build()
	}, "0 clauses must panic")

	// 2 clauses
	assert.Panics(t, func() {
		NewBuilder(TypeEthDynamicFee).
			ChainID(big.NewInt(1)).
			Clause(NewClause(&to).WithValue(big.NewInt(1))).
			Clause(NewClause(&to).WithValue(big.NewInt(2))).
			MaxFeePerGas(big.NewInt(1)).MaxPriorityFeePerGas(big.NewInt(1)).
			Gas(42_000).Nonce(0).Build()
	}, "2 clauses must panic")
}

// TestEthDynamicFee_BuildContractCreation pins that a nil-To clause lands as
// To: nil on the envelope (eth-style contract creation), and that Data is
// preserved bit-for-bit.
func TestEthDynamicFee_BuildContractCreation(t *testing.T) {
	bytecode := []byte{0x60, 0x80, 0x60, 0x40, 0x52}
	trx := NewBuilder(TypeEthDynamicFee).
		ChainID(big.NewInt(1)).
		Clause(NewClause(nil).WithData(bytecode)).
		MaxFeePerGas(big.NewInt(1)).MaxPriorityFeePerGas(big.NewInt(1)).
		Gas(100_000).Nonce(0).Build()

	cls := trx.Clauses()
	require.Len(t, cls, 1)
	assert.Nil(t, cls[0].To(), "nil-To must round-trip as contract creation")
	assert.Equal(t, bytecode, cls[0].Data())
	assert.True(t, cls[0].IsCreatingContract())
}

// TestEthDynamicFee_SignAndRecover verifies that a signature produced by
// go-ethereum's crypto.Sign (V∈{0,1}) lets Origin() recover the right address.
func TestEthDynamicFee_SignAndRecover(t *testing.T) {
	pk, err := crypto.GenerateKey()
	require.NoError(t, err)
	expected := thor.Address(crypto.PubkeyToAddress(pk.PublicKey))

	trx := newEthDynamicFeeUnsigned(t, big.NewInt(100009))
	signed, err := Sign(trx, pk)
	require.NoError(t, err)

	got, err := signed.Origin()
	require.NoError(t, err)
	assert.Equal(t, expected, got)
}

// TestEthDynamicFee_RejectsHighS pins the EIP-2 low-s requirement on 0x02.
// Flip S to N-S (the malleable counterpart, also a valid signature) and
// confirm Origin() returns ErrHighSInSignature.
func TestEthDynamicFee_RejectsHighS(t *testing.T) {
	pk, err := crypto.GenerateKey()
	require.NoError(t, err)

	signed, err := Sign(newEthDynamicFeeUnsigned(t, big.NewInt(100009)), pk)
	require.NoError(t, err)
	_, err = signed.Origin()
	require.NoError(t, err, "freshly signed tx must pass low-s")

	sig := append([]byte(nil), signed.body.signature()...)
	flipped := new(big.Int).Sub(crypto.S256().Params().N, new(big.Int).SetBytes(sig[32:64]))
	flipped.FillBytes(sig[32:64])
	sig[64] ^= 0x01 // V parity flips with S so the sig still recovers the same key

	tampered := new(Transaction)
	require.NoError(t, tampered.UnmarshalBinary(reencodeWithSig(t, signed, sig)))
	_, err = tampered.Origin()
	assert.ErrorIs(t, err, ErrHighSInSignature)
}

// reencodeWithSig re-marshals signed with sig swapped in, bypassing Sign()
// (which would re-derive a low-s signature).
func reencodeWithSig(t *testing.T, signed *Transaction, sig []byte) []byte {
	t.Helper()
	body := signed.body.(*ethDynamicFeeTransaction).copy().(*ethDynamicFeeTransaction)
	body.setSignature(sig)
	raw, err := (&Transaction{body: body}).MarshalBinary()
	require.NoError(t, err)
	return raw
}

// TestEthDynamicFee_SigningHashIsKeccak asserts the signing hash is
// Keccak256(0x02 || RLP(signingFields)).
func TestEthDynamicFee_SigningHashIsKeccak(t *testing.T) {
	trx := newEthDynamicFeeUnsigned(t, big.NewInt(42))

	var buf bytes.Buffer
	buf.WriteByte(TypeEthDynamicFee)
	err := rlp.Encode(&buf, trx.body.signingFields())
	require.NoError(t, err)
	expected := thor.Keccak256(buf.Bytes())

	assert.Equal(t, expected, trx.SigningHash())
}

// TestEthDynamicFee_IDIsEthCanonical pins ID() == Hash() == eth canonical
// txhash (Keccak256(0x02 || RLP(body))) for signed 0x02 txs.
func TestEthDynamicFee_IDIsEthCanonical(t *testing.T) {
	pk, err := crypto.GenerateKey()
	require.NoError(t, err)

	signed, err := Sign(newEthDynamicFeeUnsigned(t, big.NewInt(42)), pk)
	require.NoError(t, err)

	raw, err := signed.MarshalBinary()
	require.NoError(t, err)
	expected := thor.Keccak256(raw)

	assert.Equal(t, expected, signed.ID(), "ID()")
	assert.Equal(t, expected, signed.Hash(), "Hash()")
	assert.Equal(t, signed.ID(), signed.Hash(), "ID()==Hash() for 0x02")
}

// TestEthDynamicFee_EncodeDecodeRoundTrip checks that MarshalBinary /
// UnmarshalBinary is a bit-stable round trip.
func TestEthDynamicFee_EncodeDecodeRoundTrip(t *testing.T) {
	pk, err := crypto.GenerateKey()
	require.NoError(t, err)

	signed, err := Sign(newEthDynamicFeeUnsigned(t, big.NewInt(42)), pk)
	require.NoError(t, err)

	raw, err := signed.MarshalBinary()
	require.NoError(t, err)

	decoded := new(Transaction)
	require.NoError(t, decoded.UnmarshalBinary(raw))

	assert.Equal(t, uint8(TypeEthDynamicFee), decoded.Type())
	assert.Equal(t, signed.Hash(), decoded.Hash())
	// Re-encode and assert bit-exact match.
	rawAgain, err := decoded.MarshalBinary()
	require.NoError(t, err)
	assert.True(t, bytes.Equal(raw, rawAgain))

	origA, err := signed.Origin()
	require.NoError(t, err)
	origB, err := decoded.Origin()
	require.NoError(t, err)
	assert.Equal(t, origA, origB)
}

// TestEthDynamicFee_DecodePreservesAccessList verifies that a non-empty
// access list round-trips through RLP — the rejection happens at resolve
// time, not decode time, to keep hashes bit-exact with Ethereum wallets.
func TestEthDynamicFee_DecodePreservesAccessList(t *testing.T) {
	trx := newEthDynamicFeeUnsigned(t, big.NewInt(42))
	body := trx.body.(*ethDynamicFeeTransaction)
	body.AccessList = AccessList{
		{Address: thor.Address{0x01}, StorageKeys: []thor.Bytes32{{0x02}}},
	}

	pk, err := crypto.GenerateKey()
	require.NoError(t, err)
	signed, err := Sign(trx, pk)
	require.NoError(t, err)

	raw, err := signed.MarshalBinary()
	require.NoError(t, err)

	decoded := new(Transaction)
	require.NoError(t, decoded.UnmarshalBinary(raw))
	got := decoded.AccessList()
	require.Len(t, got, 1)
	assert.Equal(t, thor.Address{0x01}, got[0].Address)
	require.Len(t, got[0].StorageKeys, 1)
	assert.Equal(t, thor.Bytes32{0x02}, got[0].StorageKeys[0])
}

// TestEthDynamicFee_TestFeaturesIgnoresDelegation verifies that delegation
// features are simply not applicable to 0x02 (returns nil regardless of
// supported mask).
func TestEthDynamicFee_TestFeaturesIgnoresDelegation(t *testing.T) {
	trx := newEthDynamicFeeUnsigned(t, big.NewInt(42))
	var delegation Features
	delegation.SetDelegated(true)
	assert.NoError(t, trx.TestFeatures(delegation))
	assert.NoError(t, trx.TestFeatures(0))
}

// TestEthDynamicFee_SignatureLengthMustBe65 ensures the 0x02 envelope always
// carries exactly one signature.
func TestEthDynamicFee_SignatureLengthMustBe65(t *testing.T) {
	pk, err := crypto.GenerateKey()
	require.NoError(t, err)
	signed, err := Sign(newEthDynamicFeeUnsigned(t, big.NewInt(42)), pk)
	require.NoError(t, err)

	// Mutate signature length → origin recovery must fail.
	bad := signed.WithSignature(append(signed.Signature(), 0x00))
	_, err = bad.Origin()
	assert.Error(t, err)
}

// TestEthDynamicFee_SignatureVIsParity asserts that the signature's V byte is
// the parity bit (0 or 1) as required by EIP-1559, not 27/28.
func TestEthDynamicFee_SignatureVIsParity(t *testing.T) {
	pk, err := crypto.GenerateKey()
	require.NoError(t, err)
	signed, err := Sign(newEthDynamicFeeUnsigned(t, big.NewInt(42)), pk)
	require.NoError(t, err)

	sig := signed.Signature()
	require.Len(t, sig, 65)
	assert.True(t, sig[64] == 0 || sig[64] == 1, "V must be 0 or 1, got %d", sig[64])
}
