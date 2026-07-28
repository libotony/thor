// Copyright (c) 2026 The VeChainThor developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package service

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/vechain/thor/v2/api/jsonrpc/server"
)

func TestInvalidParamsError(t *testing.T) {
	err := invalidParamsError{msg: "boom"}

	var de server.DataError
	assert.True(t, errors.As(error(err), &de))
	assert.Equal(t, -32602, de.ErrorCode())
	assert.Nil(t, de.ErrorData())
	assert.Equal(t, "boom", err.Error())
}

func TestErrHeaderNotFound(t *testing.T) {
	assert.Equal(t, "header not found", errHeaderNotFound.Error())
}
