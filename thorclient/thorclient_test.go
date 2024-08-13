// Copyright (c) 2024 The VeChainThor developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package client

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/vechain/thor/v2/api/accounts"
	"github.com/vechain/thor/v2/api/transactions"
	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/tx"
)

func TestWs_Error(t *testing.T) {
	client := NewClient("http://test.com")

	for _, tc := range []struct {
		name     string
		function interface{}
	}{
		{
			name:     "SubscribeBlocks",
			function: client.SubscribeBlocks,
		},
		{
			name:     "SubscribeEvents",
			function: client.SubscribeEvents,
		},
		{
			name:     "SubscribeTransfers",
			function: client.SubscribeTransfers,
		},
		{
			name:     "SubscribeTxPool",
			function: client.SubscribeTxPool,
		},
		{
			name:     "SubscribeBeats",
			function: client.SubscribeBeats,
		},
		{
			name:     "SubscribeBeats2",
			function: client.SubscribeBeats2,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fn := reflect.ValueOf(tc.function)
			result := fn.Call([]reflect.Value{})

			if result[1].IsNil() {
				t.Errorf("expected error for %s, but got nil", tc.name)
				return
			}

			err := result[1].Interface().(error)
			assert.Error(t, err)
		})
	}
}

func TestConvertToBatchCallData(t *testing.T) {
	// Test case 1: Empty transaction
	tx1 := &tx.Transaction{}
	addr1 := &thor.Address{}
	expected1 := &accounts.BatchCallData{
		Clauses:    make(accounts.Clauses, 0),
		Gas:        0,
		ProvedWork: nil,
		Caller:     addr1,
		GasPayer:   nil,
		Expiration: 0,
		BlockRef:   "0x0000000000000000",
	}
	assert.Equal(t, expected1, convertToBatchCallData(tx1, addr1))
}

func TestRevision(t *testing.T) {
	addr := thor.BytesToAddress([]byte("account1"))
	revision := thor.BytesToBytes32([]byte("revision1"))

	for _, tc := range []struct {
		name             string
		function         interface{}
		expectedPath     string
		expectedRevision string
	}{
		{
			name:             "GetAccount",
			function:         func(client *Client) { client.GetAccount(&addr) },
			expectedPath:     "/accounts/" + addr.String(),
			expectedRevision: "",
		},
		{
			name:             "GetAccounForRevision",
			function:         func(client *Client) { client.GetAccountForRevision(&addr, &revision) },
			expectedPath:     "/accounts/" + addr.String(),
			expectedRevision: "",
		},
		{
			name:             "GetAccountCode",
			function:         func(client *Client) { client.GetAccountCode(&addr) },
			expectedPath:     "/accounts/" + addr.String() + "/code",
			expectedRevision: "",
		},
		{
			name:             "GetAccountCodeForRevision",
			function:         func(client *Client) { client.GetAccountCodeForRevision(&addr, &revision) },
			expectedPath:     "/accounts/" + addr.String() + "/code",
			expectedRevision: "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, tc.expectedPath, r.URL.Path)
				if tc.expectedRevision != "" {
					assert.Equal(t, "revision", r.URL.Query().Get("revision"))
				}

				w.Write([]byte{})
			}))
			defer ts.Close()

			client := NewClient(ts.URL)

			fn := reflect.ValueOf(tc.function)
			fn.Call([]reflect.Value{reflect.ValueOf(client)})
		})
	}
}

func TestGetTransaction(t *testing.T) {
	expectedTx := &transactions.Transaction{
		ID: thor.BytesToBytes32([]byte("txid1")),
	}

	for _, tc := range []struct {
		name      string
		function  interface{}
		isPending bool
	}{
		{
			name:      "GetTransaction",
			function:  func(client *Client) { client.GetTransaction(&expectedTx.ID) },
			isPending: false,
		},
		{
			name:      "GetPendingTransaction",
			function:  func(client *Client) { client.GetPendingTransaction(expectedTx.ID) },
			isPending: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/transactions/"+expectedTx.ID.String(), r.URL.Path)
				if tc.isPending {
					assert.Equal(t, "true", r.URL.Query().Get("pending"))
				}

				w.Write(expectedTx.ID[:])
			}))
			defer ts.Close()

			client := NewClient(ts.URL)
			fn := reflect.ValueOf(tc.function)
			fn.Call([]reflect.Value{reflect.ValueOf(client)})
		})
	}
}