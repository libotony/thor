// Copyright (c) 2026 The VeChainThor developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package service

import "errors"

const codeInvalidParams = -32602

var errHeaderNotFound = errors.New("header not found")

// invalidParamsError is a JSON-RPC invalid-params error raised by service-level validation.
type invalidParamsError struct{ msg string }

func (e invalidParamsError) Error() string  { return e.msg }
func (e invalidParamsError) ErrorCode() int { return codeInvalidParams }
func (e invalidParamsError) ErrorData() any { return nil }
