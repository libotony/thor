// Copyright (c) 2018 The VeChainThor developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package main

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"os/signal"
	"os/user"
	"path/filepath"
	"syscall"

	"github.com/ethereum/go-ethereum/crypto"
	ethlog "github.com/ethereum/go-ethereum/log"
	"github.com/mattn/go-isatty"

	"github.com/vechain/thor/v2/log"
)

func initLogger(lvl int) *slog.LevelVar {
	logLevel := log.FromLegacyLevel(lvl)
	var level slog.LevelVar
	level.Set(logLevel)
	output := io.Writer(os.Stdout)
	useColor := (isatty.IsTerminal(os.Stderr.Fd()) || isatty.IsCygwinTerminal(os.Stderr.Fd())) && os.Getenv("TERM") != "dumb"
	handler := log.NewTerminalHandlerWithLevel(output, &level, useColor)
	log.SetDefault(log.NewLogger(handler))
	ethlog.SetDefault(ethlog.NewLogger(&ethLogger{logger: log.WithContext("pkg", "geth")}))

	return &level
}

type ethLogger struct {
	logger log.Logger
}

// Enabled accepts every level; disco delegates level filtering to thor's logger.
func (h *ethLogger) Enabled(_ context.Context, _ slog.Level) bool { return true }

// Handle forwards geth log records to thor's logger. Slog attrs are dropped:
// thor's logger consumes only the message string.
func (h *ethLogger) Handle(_ context.Context, r slog.Record) error {
	switch r.Level {
	case ethlog.LevelCrit:
		h.logger.Crit(r.Message)
	case ethlog.LevelError:
		h.logger.Error(r.Message)
	case ethlog.LevelWarn:
		h.logger.Warn(r.Message)
	case ethlog.LevelInfo:
		h.logger.Info(r.Message)
	case ethlog.LevelDebug:
		h.logger.Debug(r.Message)
	case ethlog.LevelTrace:
		h.logger.Trace(r.Message)
	}
	return nil
}

func (h *ethLogger) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h *ethLogger) WithGroup(_ string) slog.Handler      { return h }

func loadOrGenerateKeyFile(keyFile string) (key *ecdsa.PrivateKey, err error) {
	if !filepath.IsAbs(keyFile) {
		if keyFile, err = filepath.Abs(keyFile); err != nil {
			return nil, err
		}
	}

	// try to load from file
	if key, err = crypto.LoadECDSA(keyFile); err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
	} else {
		return key, nil
	}

	// no such file, generate new key and write in
	key, err = crypto.GenerateKey()
	if err != nil {
		return nil, err
	}

	if err := crypto.SaveECDSA(keyFile, key); err != nil {
		return nil, err
	}
	return key, nil
}

func defaultKeyFile() string {
	return filepath.Join(mustHomeDir(), ".thor-disco.key")
}

func mustHomeDir() string {
	// try to get HOME env
	if home := os.Getenv("HOME"); home != "" {
		return home
	}

	if user, err := user.Current(); err == nil {
		if user.HomeDir != "" {
			return user.HomeDir
		}
	}

	return filepath.Base(os.Args[0])
}

func readIntFromUInt64Flag(val uint64) (int, error) {
	if val > math.MaxInt {
		return 0, fmt.Errorf("value %d is too large", val)
	}
	i := int(val)

	if i < 0 {
		return 0, fmt.Errorf("invalid value %d ", val)
	}

	return i, nil
}

func handleExitSignal() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		exitSignalCh := make(chan os.Signal, 1)
		signal.Notify(exitSignalCh, os.Interrupt, syscall.SIGTERM)

		sig := <-exitSignalCh
		log.Info("exit signal received", "signal", sig)
		cancel()
	}()
	return ctx
}
