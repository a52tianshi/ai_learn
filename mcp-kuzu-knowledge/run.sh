#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"
export PORT="${PORT:-8787}"
export DATA_DIR="${DATA_DIR:-./data}"
cargo run --release
