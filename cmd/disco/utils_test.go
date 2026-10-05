// Copyright (c) 2025 The VeChainThor developers
//
// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package main

import (
	"context"
	"io"
	"log/slog"
	"math"
	"testing"

	ethlog "github.com/ethereum/go-ethereum/log"

	"github.com/vechain/thor/v2/log"
)

func TestReadIntFromUInt64Flag_WithinRange(t *testing.T) {
	got, err := readIntFromUInt64Flag(1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != 1 {
		t.Fatalf("want 1, got %d", got)
	}
}

func TestReadIntFromUInt64Flag_MaxInt(t *testing.T) {
	val := uint64(math.MaxInt)
	got, err := readIntFromUInt64Flag(val)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != int(val) {
		t.Fatalf("want %d, got %d", val, got)
	}
}

func TestReadIntFromUInt64Flag_TooLarge(t *testing.T) {
	val := uint64(math.MaxInt) + 1
	if _, err := readIntFromUInt64Flag(val); err == nil {
		t.Fatalf("expected error for value > MaxInt")
	}
}

func TestEthLoggerEnabled(t *testing.T) {
	var lv slog.LevelVar
	lv.Set(slog.LevelInfo)
	h := &ethLogger{level: &lv}
	ctx := context.Background()
	if h.Enabled(ctx, slog.LevelDebug) {
		t.Fatal("debug should be disabled at info")
	}
	if !h.Enabled(ctx, slog.LevelWarn) {
		t.Fatal("warn should be enabled at info")
	}
	lv.Set(slog.LevelDebug)
	if !h.Enabled(ctx, slog.LevelDebug) {
		t.Fatal("debug should be enabled after Set(debug)")
	}
}

func BenchmarkEthLoggerDebug(b *testing.B) {
	var lv, allLevel slog.LevelVar
	lv.Set(slog.LevelInfo)
	allLevel.Set(slog.Level(-100))

	prev := log.Root()
	log.SetDefault(log.NewLogger(log.NewTerminalHandlerWithLevel(io.Discard, &lv, false)))
	b.Cleanup(func() { log.SetDefault(prev) })

	for name, level := range map[string]*slog.LevelVar{"accept-all": &allLevel, "filtered": &lv} {
		b.Run(name, func(b *testing.B) {
			l := ethlog.NewLogger(&ethLogger{logger: log.WithContext("pkg", "geth"), level: level})
			b.ReportAllocs()
			for b.Loop() {
				l.Debug("ping", "id", 42, "addr", "1.2.3.4:11235")
			}
		})
	}
}
