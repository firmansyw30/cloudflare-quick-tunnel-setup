#!/usr/bin/env bash
#
# run-demo.sh - start the app, wait until it is healthy, then launch a
# Cloudflare Quick Tunnel pointed at it.
#
# This prevents the most common mistake: starting cloudflared before the app is
# listening, which makes every request fail with "502 Bad Gateway / Unable to
# reach the origin service".

set -euo pipefail

ADDR="${ADDR:-http://localhost:3333}"
HEALTH_URL="${ADDR%/}/health"
BUILD_DIR="$(mktemp -d)"
APP_BIN="${BUILD_DIR}/app"
APP_PID=""
CF_PID=""

cleanup() {
	# Disable traps first so the EXIT trap does not run cleanup a second time.
	trap - EXIT INT TERM
	echo ""
	echo "Stopping demo..."
	for pid in "$CF_PID" "$APP_PID"; do
		if [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null; then
			kill "$pid" 2>/dev/null || true
			wait "$pid" 2>/dev/null || true
		fi
	done
	rm -rf "$BUILD_DIR"
}
trap cleanup EXIT INT TERM

if ! command -v cloudflared >/dev/null 2>&1; then
	echo "cloudflared not found in PATH." >&2
	echo "Install it from: https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/downloads/" >&2
	exit 1
fi

# Build once and run the binary directly, so APP_PID is the actual server and
# a signal reliably stops it (killing the "go run" wrapper alone is not enough).
echo "Building app..."
go build -o "$APP_BIN" .

echo "Starting app..."
"$APP_BIN" &
APP_PID=$!

echo "Waiting for app to become healthy at ${HEALTH_URL} ..."
ready=0
for _ in $(seq 1 30); do
	if curl -fsS "$HEALTH_URL" >/dev/null 2>&1; then
		ready=1
		break
	fi
	if ! kill -0 "$APP_PID" 2>/dev/null; then
		echo "App exited before becoming healthy." >&2
		exit 1
	fi
	sleep 0.5
done

if [[ "$ready" -ne 1 ]]; then
	echo "App did not become healthy in time; aborting." >&2
	exit 1
fi

echo "App is healthy. Starting Cloudflare Quick Tunnel..."
echo ""

# Run cloudflared in the background and wait on it, so an interrupt is handled
# immediately by the trap (foreground execution would defer the trap until
# cloudflared exits on its own).
cloudflared tunnel --url "$ADDR" &
CF_PID=$!
wait "$CF_PID"
