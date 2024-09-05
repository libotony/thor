// Copyright (c) 2024 The VeChainThor developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package wsclient

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/vechain/thor/v2/api/blocks"
	"github.com/vechain/thor/v2/api/subscriptions"
	"github.com/vechain/thor/v2/thorclient/common"
)

const readDeadline = 60 * time.Second

type Client struct {
	host   string
	scheme string
}

func NewClient(url string) (*Client, error) {
	var host string
	var scheme string

	if strings.Contains(url, "https://") || strings.Contains(url, "wss://") {
		host = strings.TrimPrefix(strings.TrimPrefix(url, "https://"), "wss://")
		scheme = "wss"
	} else if strings.Contains(url, "http://") || strings.Contains(url, "ws://") {
		host = strings.TrimPrefix(strings.TrimPrefix(url, "http://"), "ws://")
		scheme = "ws"
	} else {
		return nil, fmt.Errorf("invalid url")
	}

	return &Client{
		host:   strings.TrimSuffix(host, "/"),
		scheme: scheme,
	}, nil
}

func (c *Client) SubscribeEvents(query string) (*common.Subscription[*subscriptions.EventMessage], error) {
	conn, err := c.connect("/subscriptions/event", query)
	if err != nil {
		return nil, fmt.Errorf("unable to connect - %w", err)
	}

	eventChan := subscribe[subscriptions.EventMessage](conn)
	return &common.Subscription[*subscriptions.EventMessage]{
		EventChan: eventChan,
		Unsubscribe: func() {
			conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			conn.Close()
		},
	}, nil
}

func (c *Client) SubscribeBlocks(query string) (*common.Subscription[*blocks.JSONCollapsedBlock], error) {
	conn, err := c.connect("/subscriptions/block", query)
	if err != nil {
		return nil, fmt.Errorf("unable to connect - %w", err)
	}

	eventChan := subscribe[blocks.JSONCollapsedBlock](conn)
	return &common.Subscription[*blocks.JSONCollapsedBlock]{
		EventChan: eventChan,
		Unsubscribe: func() {
			conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			conn.Close()
		},
	}, nil
}

func (c *Client) SubscribeTransfers(query string) (*common.Subscription[*subscriptions.TransferMessage], error) {
	conn, err := c.connect("/subscriptions/transfer", query)
	if err != nil {
		return nil, fmt.Errorf("unable to connect - %w", err)
	}

	eventChan := subscribe[subscriptions.TransferMessage](conn)
	return &common.Subscription[*subscriptions.TransferMessage]{
		EventChan: eventChan,
		Unsubscribe: func() {
			conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			conn.Close()
		},
	}, nil
}

func (c *Client) SubscribeTxPool(query string) (*common.Subscription[*subscriptions.PendingTxIDMessage], error) {
	conn, err := c.connect("/subscriptions/txpool", query)
	if err != nil {
		return nil, fmt.Errorf("unable to connect - %w", err)
	}

	eventChan := subscribe[subscriptions.PendingTxIDMessage](conn)
	return &common.Subscription[*subscriptions.PendingTxIDMessage]{
		EventChan: eventChan,
		Unsubscribe: func() {
			conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			conn.Close()
		},
	}, nil
}

func (c *Client) SubscribeBeats2(query string) (*common.Subscription[*subscriptions.Beat2Message], error) {
	conn, err := c.connect("/subscriptions/beat2", query)
	if err != nil {
		return nil, fmt.Errorf("unable to connect - %w", err)
	}

	eventChan := subscribe[subscriptions.Beat2Message](conn)
	return &common.Subscription[*subscriptions.Beat2Message]{
		EventChan: eventChan,
		Unsubscribe: func() {
			conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			conn.Close()
		},
	}, nil
}

// subscribe creates a channel to handle new subscriptions
// It takes a websocket connection as an argument and returns a read-only channel for receiving messages of type T and an error if any occurs.
func subscribe[T any](conn *websocket.Conn) <-chan common.EventWrapper[*T] {
	// Create a buffered channel for events
	eventChan := make(chan common.EventWrapper[*T], 1_000)

	go func() {
		defer func() {
			close(eventChan)
			conn.Close()
		}()

		// Start a goroutine to handle receiving messages from the websocket connection
		for {
			conn.SetReadDeadline(time.Now().Add(readDeadline))
			var data T
			// Read a JSON message from the websocket and unmarshal it into data
			err := conn.ReadJSON(&data)
			// Send an EventWrapper with the error to the channel
			if err != nil {
				eventChan <- common.EventWrapper[*T]{Error: fmt.Errorf("%w: %w", common.ErrUnexpectedMsg, err)}
				return
			}

			eventChan <- common.EventWrapper[*T]{Data: &data}
		}
	}()

	return eventChan
}

func (c *Client) connect(endpoint, rawQuery string) (*websocket.Conn, error) {
	u := url.URL{
		Scheme:   c.scheme,
		Host:     c.host,
		Path:     endpoint,
		RawQuery: rawQuery,
	}

	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		return nil, err
	}
	//TODO append to the connection pool
	return conn, nil
}
