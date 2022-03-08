// Copyright (c) 2020 The VeChainThor developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package block

import (
	"errors"
	"io"

	"github.com/ethereum/go-ethereum/rlp"
)

type extension struct {
	Alpha []byte
	Vote  *Vote
}

type _extension extension

// EncodeRLP implements rlp.Encoder.
func (ex *extension) EncodeRLP(w io.Writer) error {
	if ex.Vote != nil {
		return rlp.Encode(w, (*_extension)(ex))
	}

	if len(ex.Alpha) != 0 {
		return rlp.Encode(w, []interface{}{
			ex.Alpha,
		})
	}
	return nil
}

// DecodeRLP implements rlp.Decoder.
func (ex *extension) DecodeRLP(s *rlp.Stream) error {
	var raws []rlp.RawValue

	if err := s.Decode(&raws); err != nil {
		// Error(end-of-list) means this field is not present, return default value
		// for backward compatibility
		if err == rlp.EOL {
			*ex = extension{
				nil,
				nil,
			}
			return nil
		}
	}

	var alpha []byte
	if err := rlp.DecodeBytes(raws[0], &alpha); err != nil {
		return err
	}

	// fmt.Println(len(raws))
	// fmt.Printf("0x%x\n", raws[0])
	if len(raws) == 1 {
		if len(alpha) == 0 {
			return errors.New("rlp: extension must be trimmed")
		}

		*ex = extension{
			Alpha: alpha,
			Vote:  nil,
		}
		return nil
	}

	var vote Vote
	if err := rlp.DecodeBytes(raws[1], &vote); err != nil {
		return err
	}

	*ex = extension{
		Alpha: alpha,
		Vote:  &vote,
	}
	return nil
}
