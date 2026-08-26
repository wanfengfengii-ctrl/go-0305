#!/usr/bin/env bash
# Deterministic local smoke test for the hospital isolation-bearing service.
#
# It builds the Go binary, starts the service on a local port with a temporary
# database, probes the health endpoint, exercises the design-lock API, and then
# tears down the process and every temporary file. It performs no external
# network access and does not merely invoke `go test`.
set -euo pipefail

cd "$(dirname "$0")"

PORT="${PORT:-18080}"
BIN="$(mktemp -d)/benzhi"
DB="$(mktemp -d)/benzhi.db"
SERVER_PID=""

cleanup() {
  if [[ -n "${SERVER_PID}" ]] && kill -0 "${SERVER_PID}" 2>/dev/null; then
    kill "${SERVER_PID}" 2>/dev/null || true
    wait "${SERVER_PID}" 2>/dev/null || true
  fi
  rm -rf "$(dirname "${BIN}")" "$(dirname "${DB}")"
}
trap cleanup EXIT

echo "==> Building server binary"
go build -o "${BIN}" ./cmd/server

echo "==> Starting service on :${PORT}"
LISTEN_ADDR=":${PORT}" DB_PATH="${DB}" STATIC_DIR="frontend/dist" "${BIN}" &
SERVER_PID=$!

# Wait for the health endpoint to come up (deterministic, bounded retries).
health=""
for _ in $(seq 1 50); do
  if health="$(curl -s --max-time 1 "http://127.0.0.1:${PORT}/healthz")"; then
    break
  fi
  sleep 0.1
done

if [[ "${health}" != *'"status":"ok"'* ]]; then
  echo "health check failed: ${health}" >&2
  exit 1
fi
echo "==> health ok"

# Exercise the design-lock API and assert the response deterministically.
lock_body='{
  "operation_id":"smoke-1","building":"医院A楼","unit":"隔震单元U1","summary_version":"v1",
  "transform":{"a":1,"b":0,"c":0,"d":0,"e":1,"f":0,"scale":1},
  "adjacency":[],
  "sync_unlock_group":[["P1"]],
  "positions":[{
    "building":"医院A楼","unit":"隔震单元U1","axis_grid":"1-A","position_id":"P1",
    "design_center":{"x":0,"y":0,"z":0},
    "orientation":{"x":0,"y":0,"z":1,"scale":1},
    "bearing_model":"LRB-500",
    "upper":{"id":"u1","orientation":{"x":0,"y":0,"z":1,"scale":1},"plate_width":600000,"plate_length":600000,"hole_count":4,"hole_pattern":"square-200"},
    "lower":{"id":"l1","orientation":{"x":0,"y":0,"z":-1,"scale":1},"plate_width":600000,"plate_length":600000,"hole_count":4,"hole_pattern":"square-200"},
    "allowed_eccentricity":5000,"allowed_tilt":1000,"tilt_scale":3,
    "max_shim_thickness":20000,"max_shim_layers":4
  }]
}'

lock_resp="$(curl -s -X POST "http://127.0.0.1:${PORT}/api/v1/isolation-units" \
  -H 'Content-Type: application/json' -d "${lock_body}")"

if [[ "${lock_resp}" != *'"lock_digest"'* ]]; then
  echo "lock failed: ${lock_resp}" >&2
  exit 1
fi
echo "==> design lock ok"

# Read the unit view and assert the position stage matrix is present.
unit_resp="$(curl -s "http://127.0.0.1:${PORT}/api/v1/units/%E9%9A%94%E9%9C%87%E5%8D%95%E5%85%83U1")"
if [[ "${unit_resp}" != *'"positions"'* ]]; then
  echo "unit view failed: ${unit_resp}" >&2
  exit 1
fi
echo "==> unit view ok"

echo "==> smoke test passed"
