// Copyright (c) 2018 The VeChainThor developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package block

import (
	"crypto/rand"
	"sync/atomic"
	"testing"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/stretchr/testify/assert"
	"github.com/vechain/thor/v2/thor"
)

func TestHeader_BetterThan(t *testing.T) {
	type fields struct {
		body  headerBody
		cache struct {
			signingHash atomic.Value
			id          atomic.Value
			pubkey      atomic.Value
			beta        atomic.Value
		}
	}
	type args struct {
		other *Header
	}

	var (
		largerID  fields
		smallerID fields
	)
	largerID.cache.id.Store(thor.Bytes32{1})
	smallerID.cache.id.Store(thor.Bytes32{0})

	tests := []struct {
		name   string
		fields fields
		args   args
		want   bool
	}{
		{"higher score", fields{body: headerBody{TotalScore: 10}}, args{other: &Header{body: headerBody{TotalScore: 9}}}, true},
		{"lower score", fields{body: headerBody{TotalScore: 9}}, args{other: &Header{body: headerBody{TotalScore: 10}}}, false},
		{"equal score, larger id", largerID, args{&Header{smallerID.body, smallerID.cache}}, false},
		{"equal score, smaller id", smallerID, args{&Header{largerID.body, largerID.cache}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &Header{
				body:  tt.fields.body,
				cache: tt.fields.cache,
			}
			if got := h.BetterThan(tt.args.other); got != tt.want {
				t.Errorf("Header.BetterThan() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHeaderEncoding(t *testing.T) {
	var sig [65]byte
	rand.Read(sig[:])

	block := new(Builder).Build().WithSignature(sig[:])
	h := block.Header()

	bytes, err := rlp.EncodeToBytes(h)
	if err != nil {
		t.Fatal(err)
	}

	var hh Header
	err = rlp.DecodeBytes(bytes, &hh)
	if err != nil {
		t.Fatal(err)
	}

	bytes = append(bytes, []byte("just trailing")...)
	var hhh Header
	err = rlp.DecodeBytes(bytes, &hhh)
	assert.EqualError(t, err, "rlp: input contains more than one value")

	var proof [81]byte
	var alpha [32]byte
	rand.Read(proof[:])
	rand.Read(alpha[:])

	cplx, err := NewComplexSignature(sig[:], proof[:])
	if err != nil {
		t.Fatal(err)
	}

	b1 := new(Builder).Alpha(alpha[:]).Build().WithSignature(cplx[:])
	bs1, err := rlp.EncodeToBytes(b1.Header())
	if err != nil {
		t.Fatal(err)
	}

	var h1 Header
	err = rlp.DecodeBytes(bs1, &h1)
	if err != nil {
		t.Fatal(err)
	}
}

// type extension struct{Alpha []byte}
func TestEncodingBadExtension(t *testing.T) {
	var sig [65]byte
	rand.Read(sig[:])

	block := new(Builder).Build().WithSignature(sig[:])
	h := block.Header()

	bytes, err := rlp.EncodeToBytes(h)
	if err != nil {
		t.Fatal(err)
	}

	var h1 Header
	err = rlp.DecodeBytes(bytes, &h1)
	if err != nil {
		t.Fatal(err)
	}

	data, _, err := rlp.SplitList(bytes)
	if err != nil {
		t.Fatal(err)
	}
	count, err := rlp.CountValues(data)
	if err != nil {
		t.Fatal(err)
	}
	// backward compatiability，required to be trimmed
	assert.EqualValues(t, 10, count)

	var raws []rlp.RawValue
	_ = rlp.DecodeBytes(bytes, &raws)
	d, _ := rlp.EncodeToBytes(&struct {
		Alpha []byte
	}{
		[]byte{},
	})
	raws = append(raws, d)
	b, _ := rlp.EncodeToBytes(raws)

	var h2 Header
	err = rlp.DecodeBytes(b, &h2)

	assert.EqualError(t, err, "rlp: extension must be trimmed")
}

// type extension struct{Alpha []byte}
func TestEncodingExtension(t *testing.T) {
	var sig [ComplexSigSize]byte
	var alpha [32]byte
	rand.Read(sig[:])
	rand.Read(alpha[:])

	block := new(Builder).Alpha(alpha[:]).Build().WithSignature(sig[:])
	h := block.Header()

	bytes, err := rlp.EncodeToBytes(h)
	if err != nil {
		t.Fatal(err)
	}

	var hh Header
	err = rlp.DecodeBytes(bytes, &hh)
	if err != nil {
		t.Fatal(err)
	}

	data, _, err := rlp.SplitList(bytes)
	if err != nil {
		t.Fatal(err)
	}
	count, err := rlp.CountValues(data)
	if err != nil {
		t.Fatal(err)
	}
	assert.EqualValues(t, 11, count)
}

// decoding block that generated by the earlier version
func TestCodingCompatibility(t *testing.T) {
	raw := hexutil.MustDecode("0xf8e0a0000000000000000000000000000000000000000000000000000000000000000080809400000000000000000000000000000000000000008080a045b0cfc220ceec5b7c1c62c4d4193d38e4eba48e8815729ce75f9c0ab0e4c1c0a00000000000000000000000000000000000000000000000000000000000000000a00000000000000000000000000000000000000000000000000000000000000000b841e95a07bda136baa1181f32fba25b8dec156dee373781fdc7d24acd5e60ebc104c04b397ee7a67953e2d10acc4835343cd949a73e7e58db1b92f682db62e793c412")

	var h0 Header
	err := rlp.DecodeBytes(raw, &h0)
	if err != nil {
		t.Fatal(err)
	}

	bytes, err := rlp.EncodeToBytes(&h0)
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, raw, bytes)

	data, _, err := rlp.SplitList(bytes)
	if err != nil {
		t.Fatal(err)
	}
	count, err := rlp.CountValues(data)
	if err != nil {
		t.Fatal(err)
	}
	assert.EqualValues(t, 10, count)
}

type v2 struct {
	Extension extension
}

// type extension struct{Alpha []byte; Vote *Vote}
func TestExtensionV2(t *testing.T) {
	tests := []struct {
		name string
		test func(*testing.T)
	}{
		{
			name: "default value",
			test: func(t *testing.T) {
				bytes, err := rlp.EncodeToBytes(&v2{
					Extension: extension{},
				})
				assert.Nil(t, err)

				content, _, err := rlp.SplitList(bytes)
				assert.Nil(t, err)

				cnt, err := rlp.CountValues(content)
				assert.Nil(t, err)

				assert.Equal(t, 0, cnt)

				var dst v2
				assert.Nil(t, rlp.DecodeBytes(bytes, &dst))
			},
		},
		{
			name: "regular",
			test: func(t *testing.T) {
				bytes, err := rlp.EncodeToBytes(&v2{
					Extension: extension{
						Alpha: thor.Bytes32{}.Bytes(),
						COM:   true,
					},
				})
				assert.Nil(t, err)

				content, _, err := rlp.SplitList(bytes)
				assert.Nil(t, err)

				cnt, err := rlp.CountValues(content)
				assert.Nil(t, err)

				assert.Equal(t, 1, cnt)

				var dst v2
				err = rlp.DecodeBytes(bytes, &dst)
				assert.Nil(t, err)

				assert.Equal(t, thor.Bytes32{}.Bytes(), dst.Extension.Alpha)
				assert.True(t, dst.Extension.COM)
			},
		},
		{
			name: "only alpha",
			test: func(t *testing.T) {
				type v2x struct {
					Extension struct {
						Alpha []byte
					}
				}

				bytes, err := rlp.EncodeToBytes(&v2x{
					Extension: struct{ Alpha []byte }{
						Alpha: []byte{},
					},
				})
				assert.Nil(t, err)

				var dst v2
				err = rlp.DecodeBytes(bytes, &dst)
				assert.EqualError(t, err, "rlp: extension must be trimmed")

				assert.False(t, dst.Extension.COM)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.test(t)
		})
	}
}
