// Copyright (c) 2024 The VeChainThor developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package httpclient

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/ethereum/go-ethereum/common/math"
	"github.com/stretchr/testify/assert"
	"github.com/vechain/thor/v2/api/accounts"
	"github.com/vechain/thor/v2/api/blocks"
	"github.com/vechain/thor/v2/api/events"
	"github.com/vechain/thor/v2/api/node"
	"github.com/vechain/thor/v2/api/transactions"
	"github.com/vechain/thor/v2/api/transfers"
	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/thorclient/common"
)

func TestClient_GetTransactionReceipt(t *testing.T) {
	txID := thor.Bytes32{0x01}
	expectedReceipt := &transactions.Receipt{
		GasUsed:  1000,
		GasPayer: thor.Address{0x01},
		Paid:     &math.HexOrDecimal256{},
		Reward:   &math.HexOrDecimal256{},
		Reverted: false,
		Meta:     transactions.ReceiptMeta{},
		Outputs:  []*transactions.Output{},
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/transactions/"+txID.String()+"/receipt", r.URL.Path)

		receiptBytes, _ := json.Marshal(expectedReceipt)
		w.Write(receiptBytes)
	}))
	defer ts.Close()

	client := NewClient(ts.URL)
	receipt, err := client.GetTransactionReceipt(&txID)

	assert.NoError(t, err)
	assert.Equal(t, expectedReceipt, receipt)
}

func TestClient_InspectClauses(t *testing.T) {
	calldata := &accounts.BatchCallData{}
	expectedResults := []*accounts.CallResult{{
		Data:      "data",
		Events:    []*transactions.Event{},
		Transfers: []*transactions.Transfer{},
		GasUsed:   1000,
		Reverted:  false,
		VMError:   "no error"}}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/accounts/*", r.URL.Path)

		inspectionResBytes, _ := json.Marshal(expectedResults)
		w.Write(inspectionResBytes)
	}))
	defer ts.Close()

	client := NewClient(ts.URL)
	results, err := client.InspectClauses(calldata)

	assert.NoError(t, err)
	assert.Equal(t, expectedResults, results)
}

func TestClient_SendTransaction(t *testing.T) {
	rawTx := &transactions.RawTx{}
	expectedResult := &common.TxSendResult{ID: &thor.Bytes32{0x01}}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/transactions", r.URL.Path)

		txIDBytes, _ := json.Marshal(expectedResult)
		w.Write(txIDBytes)
	}))
	defer ts.Close()

	client := NewClient(ts.URL)
	result, err := client.SendTransaction(rawTx)

	assert.NoError(t, err)
	assert.Equal(t, expectedResult, result)
}

func TestClient_GetLogTransfer(t *testing.T) {
	req := map[string]interface{}{}
	expectedTransfers := []*transfers.FilteredTransfer{{
		Sender:    thor.Address{0x01},
		Recipient: thor.Address{0x02},
		Amount:    &math.HexOrDecimal256{},
		Meta:      transfers.LogMeta{},
	}}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/logs/transfer", r.URL.Path)

		filteredTransfersBytes, _ := json.Marshal(expectedTransfers)
		w.Write(filteredTransfersBytes)
	}))
	defer ts.Close()

	client := NewClient(ts.URL)
	transfers, err := client.GetLogTransfer(req)

	assert.NoError(t, err)
	assert.Equal(t, expectedTransfers, transfers)
}

func TestClient_GetLogsEvent(t *testing.T) {
	req := map[string]interface{}{}
	expectedEvents := []events.FilteredEvent{{
		Address: thor.Address{0x01},
		Topics:  []*thor.Bytes32{{0x01}},
		Data:    "data",
		Meta:    events.LogMeta{},
	}}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/logs/event", r.URL.Path)

		filteredEventsBytes, _ := json.Marshal(expectedEvents)
		w.Write(filteredEventsBytes)
	}))
	defer ts.Close()

	client := NewClient(ts.URL)
	events, err := client.GetLogsEvent(req)

	assert.NoError(t, err)
	assert.Equal(t, expectedEvents, events)
}

func TestClient_GetAccount(t *testing.T) {
	addr := thor.Address{0x01}
	expectedAccount := &accounts.Account{
		Balance: math.HexOrDecimal256{},
		Energy:  math.HexOrDecimal256{},
		HasCode: false,
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/accounts/"+addr.String(), r.URL.Path)

		accountBytes, _ := json.Marshal(expectedAccount)
		w.Write(accountBytes)
	}))
	defer ts.Close()

	client := NewClient(ts.URL)
	account, err := client.GetAccount(&addr, &thor.Bytes32{})

	assert.NoError(t, err)
	assert.Equal(t, expectedAccount, account)
}

func TestClient_GetAccountCode(t *testing.T) {
	addr := thor.Address{0x01}
	expectedByteCode := []byte{0x01}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/accounts/"+addr.String()+"/code", r.URL.Path)

		w.Write(expectedByteCode)
	}))
	defer ts.Close()

	client := NewClient(ts.URL)
	byteCode, err := client.GetAccountCode(&addr, &thor.Bytes32{})

	assert.NoError(t, err)
	assert.Equal(t, expectedByteCode, byteCode)
}

func TestClient_GetStorage(t *testing.T) {
	addr := thor.Address{0x01}
	key := thor.Bytes32{0x01}
	expectedData := []byte{0x01}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/accounts/"+addr.String()+"/key/"+key.String(), r.URL.Path)

		w.Write(expectedData)
	}))
	defer ts.Close()

	client := NewClient(ts.URL)
	data, err := client.GetStorage(&addr, &key)

	assert.NoError(t, err)
	assert.Equal(t, expectedData, data)
}

func TestClient_GetExpandedBlock(t *testing.T) {
	blockID := "123"
	expectedBlock := &blocks.JSONExpandedBlock{}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/blocks/"+blockID+"?expanded=true", r.URL.Path+"?"+r.URL.RawQuery)

		blockBytes, _ := json.Marshal(expectedBlock)
		w.Write(blockBytes)
	}))
	defer ts.Close()

	client := NewClient(ts.URL)
	block, err := client.GetExpandedBlock(blockID)

	assert.NoError(t, err)
	assert.Equal(t, expectedBlock, block)
}

func TestClient_GetBlock(t *testing.T) {
	blockID := "123"
	expectedBlock := &blocks.JSONBlockSummary{
		Number:      123456,
		ID:          thor.Bytes32{0x01},
		GasLimit:    1000,
		Beneficiary: thor.Address{0x01},
		GasUsed:     100,
		TxsRoot:     thor.Bytes32{0x03},
		TxsFeatures: 1,
		IsFinalized: false,
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/blocks/"+blockID, r.URL.Path)

		blockBytes, _ := json.Marshal(expectedBlock)
		w.Write(blockBytes)
	}))
	defer ts.Close()

	client := NewClient(ts.URL)
	block, err := client.GetBlock(blockID)

	assert.NoError(t, err)
	assert.Equal(t, expectedBlock, block)
}

func TestClient_GetTransaction(t *testing.T) {
	txID := thor.Bytes32{0x01}
	expectedTx := &transactions.Transaction{ID: txID}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/transactions/"+txID.String(), r.URL.Path)

		txBytes, _ := json.Marshal(expectedTx)
		w.Write(txBytes)
	}))
	defer ts.Close()

	client := NewClient(ts.URL)
	tx, err := client.GetTransaction(&txID, false)

	assert.NoError(t, err)
	assert.Equal(t, expectedTx, tx)
}

func TestClient_RawHTTPPost(t *testing.T) {
	url := "/test"
	calldata := map[string]interface{}{}
	expectedResponse := []byte{0x01}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, url, r.URL.Path)

		w.Write(expectedResponse)
	}))
	defer ts.Close()

	client := NewClient(ts.URL)
	response, err := client.RawHTTPPost(url, calldata)

	assert.NoError(t, err)
	assert.Equal(t, expectedResponse, response)
}

func TestClient_RawHTTPGet(t *testing.T) {
	url := "/test"
	expectedResponse := []byte{0x01}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, url, r.URL.Path)

		w.Write(expectedResponse)
	}))
	defer ts.Close()

	client := NewClient(ts.URL)
	response, err := client.RawHTTPGet(url)

	assert.NoError(t, err)
	assert.Equal(t, expectedResponse, response)
}

func TestClient_GetPeers(t *testing.T) {
	expectedPeers := []*node.PeerStats{{
		Name:        "nodeA",
		BestBlockID: thor.Bytes32{0x01},
		TotalScore:  1000,
		PeerID:      "peerId",
		NetAddr:     "netAddr",
		Inbound:     false,
		Duration:    1000,
	}}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/node/network/peers", r.URL.Path)

		peersBytes, _ := json.Marshal(expectedPeers)
		w.Write(peersBytes)
	}))
	defer ts.Close()

	client := NewClient(ts.URL)
	peers, err := client.GetPeers()

	assert.NoError(t, err)
	assert.Equal(t, expectedPeers, peers)
}

func TestClient_Errors(t *testing.T) {
	txID := thor.Bytes32{0x01}
	blockID := "123"
	addr := thor.Address{0x01}

	for _, tc := range []struct {
		name     string
		path     string
		function interface{}
	}{
		{
			name:     "GetTransactionReceipt",
			path:     "/transactions/" + txID.String() + "/receipt",
			function: func(client *Client) (*transactions.Receipt, error) { return client.GetTransactionReceipt(&txID) },
		},
		{
			name: "InspectClauses",
			path: "/accounts/*",
			function: func(client *Client) ([]*accounts.CallResult, error) {
				return client.InspectClauses(&accounts.BatchCallData{})
			},
		},
		{
			name: "SendTransaction",
			path: "/transactions",
			function: func(client *Client) (*common.TxSendResult, error) {
				return client.SendTransaction(&transactions.RawTx{})
			},
		},
		{
			name: "GetLogTransfer",
			path: "/logs/transfer",
			function: func(client *Client) ([]*transfers.FilteredTransfer, error) {
				return client.GetLogTransfer(map[string]interface{}{})
			},
		},
		{
			name: "GetLogsEvent",
			path: "/logs/event",
			function: func(client *Client) ([]events.FilteredEvent, error) {
				return client.GetLogsEvent(map[string]interface{}{})
			},
		},
		{
			name:     "GetAccount",
			path:     "/accounts/" + addr.String(),
			function: func(client *Client) (*accounts.Account, error) { return client.GetAccount(&addr, &thor.Bytes32{}) },
		},
		{
			name:     "GetContractByteCode",
			path:     "/accounts/" + addr.String() + "/code",
			function: func(client *Client) ([]byte, error) { return client.GetAccountCode(&addr, &thor.Bytes32{}) },
		},
		{
			name:     "GetStorage",
			path:     "/accounts/" + addr.String() + "/key/" + thor.Bytes32{}.String(),
			function: func(client *Client) ([]byte, error) { return client.GetStorage(&addr, &thor.Bytes32{}) },
		},
		{
			name:     "GetExpandedBlock",
			path:     "/blocks/" + blockID + "?expanded=true",
			function: func(client *Client) (*blocks.JSONExpandedBlock, error) { return client.GetExpandedBlock(blockID) },
		},
		{
			name:     "GetBlock",
			path:     "/blocks/" + blockID,
			function: func(client *Client) (*blocks.JSONBlockSummary, error) { return client.GetBlock(blockID) },
		},
		{
			name:     "GetTransaction",
			path:     "/transactions/" + txID.String(),
			function: func(client *Client) (*transactions.Transaction, error) { return client.GetTransaction(&txID, false) },
		},
		{
			name:     "RawHTTPPost",
			path:     "/test",
			function: func(client *Client) ([]byte, error) { return client.RawHTTPPost("/test", map[string]interface{}{}) },
		},
		{
			name:     "RawHTTPGet",
			path:     "/test",
			function: func(client *Client) ([]byte, error) { return client.RawHTTPGet("/test") },
		},
		{
			name:     "GetPeers",
			path:     "/node/network/peers",
			function: func(client *Client) ([]*node.PeerStats, error) { return client.GetPeers() },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Contains(t, tc.path, r.URL.Path)

				w.WriteHeader(http.StatusInternalServerError)
			}))
			defer ts.Close()

			client := NewClient(ts.URL)

			fn := reflect.ValueOf(tc.function)
			result := fn.Call([]reflect.Value{reflect.ValueOf(client)})

			if result[1].IsNil() {
				t.Errorf("expected error for %s, but got nil", tc.name)
				return
			}

			err := result[1].Interface().(error)
			assert.Error(t, err)
		})
	}
}