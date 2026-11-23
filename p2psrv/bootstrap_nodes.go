// Copyright (c) 2021 The VeChainThor developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package p2psrv

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/ethereum/go-ethereum/p2p/enode"
)

const maxBootnodeListBytes = 1 << 20

const maxLoggedSkips = 5

// parseBootnodeList reads a list of node URLs, one per line:
//
//	# comment
//	enode://… or enr:…
//
// Lines that cannot be used (unparsable, incomplete or unusable address) are logged and
// skipped; they never fail the whole list.
func parseBootnodeList(r io.Reader) []*enode.Node {
	var nodes []*enode.Node

	data, err := io.ReadAll(io.LimitReader(r, maxBootnodeListBytes))
	if err != nil {
		logger.Warn("bootnode list read failed, using what was read", "err", err)
	}
	if len(data) == maxBootnodeListBytes {
		// the last line may be cut mid-way, so it is dropped
		if i := strings.LastIndexByte(string(data), '\n'); i >= 0 {
			data = data[:i]
		}
		logger.Warn("bootnode list reached size limit, ignoring the rest", "limit", maxBootnodeListBytes)
	}

	skipped := 0
	// A bad file (captive portal page, corruption) must not flood the log.
	skip := func(lineNo int, reason string) {
		if skipped++; skipped <= maxLoggedSkips {
			logger.Warn("bootnode list: line skipped", "line", lineNo, "reason", reason)
		}
	}
	for i, line := range strings.Split(string(data), "\n") {
		lineNo := i + 1
		line = strings.TrimSpace(line)
		switch {
		case line == "" || strings.HasPrefix(line, "#"):
		default:
			n, err := enode.Parse(enode.ValidSchemes, line)
			if err != nil {
				skip(lineNo, "unparsable node")
				continue
			}
			if err := n.ValidateComplete(); err != nil {
				skip(lineNo, "incomplete node")
				continue
			}
			// ValidateComplete misses the IPv4-in-IPv6 form of these, which the legacy stack rejects.
			if ip := n.IPAddr().Unmap(); ip.IsUnspecified() || ip.IsMulticast() {
				skip(lineNo, "unspecified or multicast address")
				continue
			}
			nodes = append(nodes, n)
		}
	}
	if skipped > maxLoggedSkips {
		logger.Warn("bootnode list: lines skipped", "total", skipped)
	}
	return nodes
}

// fetchBootnodeList downloads and parses the list. It fails on transport errors, body read errors and non-200 responses.
func fetchBootnodeList(ctx context.Context, url string) ([]*enode.Node, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http fetch failed: statusCode=%d", resp.StatusCode)
	}
	// A read error must fail the fetch: a truncated body can hold a node with a wrong port.
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBootnodeListBytes))
	if err != nil {
		return nil, err
	}
	return parseBootnodeList(bytes.NewReader(data)), nil
}
