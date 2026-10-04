#!/usr/bin/env bash
# Usage: scripts/demo.sh rest|grpc   -> prints success, timeout, and still-alive evidence
set -u
mode=${1:?usage: demo.sh rest|grpc}
cd "$(dirname "$0")/.."
mkdir -p evidence
go build -o bin/ ./cmd/...
if [ "$mode" = rest ]; then ORDER_PORT=8080; else ORDER_PORT=8082; fi
order() { curl -s -w 'HTTP %{http_code}\n' -X POST localhost:$ORDER_PORT/orders \
  -H 'Content-Type: application/json' -d "$1"; }
pids=()
cleanup() { kill "${pids[@]}" 2>/dev/null; }
trap cleanup EXIT

echo "== 1. fast inventory (success) =="
bin/$mode-inventory 2>evidence/$mode-inventory.log & pids+=($!)
ORDER_TIMEOUT_MS=500 bin/$mode-order 2>evidence/$mode-order.log & pids+=($!)
sleep 1
order '{"item_id":"widget","quantity":2}'

echo "== 2. unavailable cases =="
order '{"item_id":"gadget","quantity":1}'
order '{"item_id":"nope","quantity":1}'

echo "== 3. slow inventory (2000ms delay > 500ms limit) =="
kill "${pids[0]}"; sleep 0.5
INVENTORY_DELAY_MS=2000 bin/$mode-inventory 2>>evidence/$mode-inventory.log & pids[0]=$!
sleep 1
order '{"item_id":"widget","quantity":2}'

echo "== 4. order service still running =="
kill -0 "${pids[1]}" && echo "order service PID ${pids[1]} alive"
order '{"item_id":"widget","quantity":0}'

echo "== order service log =="
cat evidence/$mode-order.log
