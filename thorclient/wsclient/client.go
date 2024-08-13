// Copyright (c) 2024 The VeChainThor developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package wsclient

import (
	"fmt"
	"log"
	"net/url"
	"strings"

	"github.com/gorilla/websocket"
	"github.com/vechain/thor/v2/api/blocks"
	"github.com/vechain/thor/v2/api/subscriptions"
)

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

func (c *Client) SubscribeEvents(query string) (<-chan *subscriptions.EventMessage, error) {
	conn, err := c.connect("/subscriptions/event", query)
	if err != nil {
		return nil, fmt.Errorf("unable to connect - %w", err)
	}

	return subscribe[subscriptions.EventMessage](conn)
}

func (c *Client) SubscribeBlocks(query string) (<-chan *blocks.JSONBlockSummary, error) {
	conn, err := c.connect("/subscriptions/block", query)
	if err != nil {
		return nil, fmt.Errorf("unable to connect - %w", err)
	}

	return subscribe[blocks.JSONBlockSummary](conn)
}

func (c *Client) SubscribeTransfers(query string) (<-chan *subscriptions.TransferMessage, error) {
	conn, err := c.connect("/subscriptions/transfer", query)
	if err != nil {
		return nil, fmt.Errorf("unable to connect - %w", err)
	}

	return subscribe[subscriptions.TransferMessage](conn)
}

func (c *Client) SubscribeBeats(query string) (<-chan *subscriptions.BeatMessage, error) {
	conn, err := c.connect("/subscriptions/beats", query)
	if err != nil {
		return nil, fmt.Errorf("unable to connect - %w", err)
	}

	return subscribe[subscriptions.BeatMessage](conn)
}

func (c *Client) SubscribeTxPool(query string) (<-chan *subscriptions.PendingTxIDMessage, error) {
	conn, err := c.connect("/subscriptions/txpool", query)
	if err != nil {
		return nil, fmt.Errorf("unable to connect - %w", err)
	}

	return subscribe[subscriptions.PendingTxIDMessage](conn)
}

func (c *Client) SubscribeBeats2(query string) (<-chan *subscriptions.Beat2Message, error) {
	conn, err := c.connect("/subscriptions/beat2", query)
	if err != nil {
		return nil, fmt.Errorf("unable to connect - %w", err)
	}

	return subscribe[subscriptions.Beat2Message](conn)
}

// subscribe creates a channel to handle new subscriptions
// It takes a websocket connection as an argument and returns a read-only channel for receiving messages of type T and an error if any occurs.
func subscribe[T any](conn *websocket.Conn) (<-chan *T, error) {
	// Create a new channel for events
	eventChan := make(chan *T)

	// Start a goroutine to handle receiving messages from the websocket connection
	go func() {
		defer close(eventChan)
		defer conn.Close()

		for {
			var data T
			// Read a JSON message from the websocket and unmarshal it into data
			err := conn.ReadJSON(&data)
			if err != nil {
				// Log the error and exit the loop if reading fails
				log.Println("Read error:", err)
				return
			}

			// Send the received data to the event channel
			eventChan <- &data
			// TODO: handle the case where data is invalid or undesirable
		}
	}()

	// Return the event channel
	return eventChan, nil
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