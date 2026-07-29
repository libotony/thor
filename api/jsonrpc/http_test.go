// Copyright (c) 2026 The VeChainThor developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package jsonrpc

import (
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vechain/thor/v2/genesis"
	"github.com/vechain/thor/v2/test/testchain"
)

func newHTTPServer(t *testing.T) *httptest.Server {
	tc, err := testchain.NewDefault()
	require.NoError(t, err)
	router := mux.NewRouter()
	New(tc.Repo(), tc.Stater(), tc.Engine()).Mount(router, "/rpc")
	return httptest.NewServer(router)
}

func post(t *testing.T, url, body string) string {
	resp, err := http.Post(url, "application/json", strings.NewReader(body)) //#nosec G107
	require.NoError(t, err)
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return string(out)
}

func TestHTTPSingleAndBatch(t *testing.T) {
	ts := newHTTPServer(t)
	defer ts.Close()
	url := ts.URL + "/rpc"

	// single: blockNumber on genesis-only chain
	assert.JSONEq(t, `{"jsonrpc":"2.0","id":1,"result":"0x0"}`,
		post(t, url, `{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber","params":[]}`))

	// batch of two
	assert.JSONEq(
		t,
		`[{"jsonrpc":"2.0","id":1,"result":"0x0"},{"jsonrpc":"2.0","id":2,"result":"0x0"}]`,
		post(
			t,
			url,
			`[{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber"},{"jsonrpc":"2.0","id":2,"method":"eth_blockNumber"}]`,
		),
	)

	// unknown method -> -32601
	resp := post(t, url, `{"jsonrpc":"2.0","id":9,"method":"eth_nope"}`)
	assert.Contains(t, resp, "-32601")
}

func TestHTTPGetBalance(t *testing.T) {
	ts := newHTTPServer(t)
	defer ts.Close()
	url := ts.URL + "/rpc"

	addr := genesis.DevAccounts()[0].Address.String()
	want, _ := new(big.Int).SetString(genesis.InitialDevAccountBalance, 10)

	// block param omitted -> latest
	assert.JSONEq(t,
		fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"result":"%s"}`, (*hexutil.Big)(want).String()),
		post(t, url, fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"eth_getBalance","params":["%s"]}`, addr)))

	// thor tag rejected
	resp := post(t, url, fmt.Sprintf(`{"jsonrpc":"2.0","id":2,"method":"eth_getBalance","params":["%s","best"]}`, addr))
	assert.Contains(t, resp, "-32602")
	assert.NotContains(t, resp, "result")
}
