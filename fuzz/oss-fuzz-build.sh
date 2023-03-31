#!/bin/bash

set -euo pipefail

export FUZZ_ROOT="github.com/vechain/thor"

build_go_fuzzer() {
	local function="$1"
	local fuzzer="$2"

	compile_native_go_fuzzer "$FUZZ_ROOT"/fuzz/tests "$function" "$fuzzer"
}

build_go_fuzzer FuzzBlock fuzz_block