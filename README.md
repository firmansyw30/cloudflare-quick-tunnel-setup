# Cloudflare Quick Tunnel Setup — JSON HTTP API Demo

A small, standard-library-only Go HTTP server that exposes a JSON API. It exists
as a **proof of concept for tunneling**: the app listens on one port, you point a
Cloudflare Quick Tunnel at it, and you verify that the public `trycloudflare.com`
URL reaches your local handlers.

Based on the concepts from DigitalOcean's
[How To Make an HTTP Server in Go](https://www.digitalocean.com/community/tutorials/how-to-make-an-http-server-in-go),
trimmed down to a single clean server for the tunneling use case.

## What it demonstrates

- Explicit `http.NewServeMux()` with Go 1.22+ method-aware route patterns
- Struct-based JSON decoding with validation and a uniform error envelope
- `GET /health` as the simplest endpoint to confirm a tunnel is working
- Graceful shutdown on Ctrl-C (`signal.NotifyContext` + `server.Shutdown`)

## Requirements

- Go 1.22 or newer (uses method-aware routing patterns)

## Run

```bash
go run .
```

The server listens on `http://localhost:3333`.

## Endpoints

| Method | Path      | Response                              | Status |
| ------ | --------- | ------------------------------------- | ------ |
| GET    | `/`       | `This is my website!` (plain text)    | 200    |
| GET    | `/hello`  | `Hello, HTTP!` (plain text)           | 200    |
| GET    | `/health` | `{"status":"ok"}`                     | 200    |
| POST   | `/json`   | success envelope (see below)          | 200    |
| POST   | `/json`   | error envelope (bad JSON)             | 400    |
| POST   | `/json`   | error envelope (empty name/body)      | 422    |
| —      | known path, wrong method | `Allow` header + error envelope | 405 |
| —      | unknown path | error envelope                     | 404    |

### Response shapes

Success (`POST /json`):

```json
{
  "status": "success",
  "message": "Received message from Ada",
  "body": "Hello"
}
```

Error (any failing path):

```json
{
  "status": "error",
  "error": "invalid JSON payload",
  "detail": "invalid character 'b' looking for beginning of object key string"
}
```

Request body for `POST /json`:

```json
{
  "name": "Ada",
  "body": "Hello"
}
```

## Local curl examples

```bash
# Plain text routes
curl -s localhost:3333/
curl -s localhost:3333/hello

# Health check
curl -s localhost:3333/health

# Valid JSON payload
curl -s -X POST localhost:3333/json \
  -H 'Content-Type: application/json' \
  -d '{"name":"Ada","body":"Hello"}'

# Malformed JSON -> 400
curl -s -X POST localhost:3333/json -d '{bad json'

# Validation failure -> 422
curl -s -X POST localhost:3333/json -d '{"name":"","body":"Hello"}'

# Wrong method -> 405 with Allow header
curl -s -i -X PUT localhost:3333/json

# Unknown path -> 404
curl -s -i localhost:3333/nope
```

## Tunnel demo (the actual proof of concept)

### Prerequisites

- Install `cloudflared` from the
  [official downloads page](https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/downloads/).
- No Cloudflare account is required for Quick Tunnels.
- The generated `*.trycloudflare.com` URL is **public** and changes on every
  restart. Treat it like a temporary, unauthenticated link.

### Option A — one-shot script (recommended)

`run-demo.sh` starts the app, waits until it is healthy, then launches the
tunnel. This avoids the most common failure, where cloudflared starts before the
app is listening.

```bash
./run-demo.sh
```

It prints the `trycloudflare.com` URL once the tunnel is up. Press `Ctrl-C` to
stop both the tunnel and the app.

### Option B — two terminals (manual)

Tunnel demos need **two processes running at the same time**. Keep both alive.

1. **Terminal A** — start the app and leave it running:

   ```bash
   go run .
   ```

2. **Preflight check** — confirm the app answers before touching the tunnel.
   This is what prevents the 502 error:

   ```bash
   curl -s localhost:3333/health   # must print {"status":"ok"}
   ```

3. **Terminal B** — start a Quick Tunnel pointing at the local port and leave it
   running:

   ```bash
   cloudflared tunnel --url http://localhost:3333
   ```

   `cloudflared` prints a public URL like
   `https://random-words-1234.trycloudflare.com`.

4. Run the same requests against the public URL to prove the tunnel reaches the
   app end to end:

   ```bash
   export TUNNEL_URL="https://random-words-1234.trycloudflare.com"

   curl -s "$TUNNEL_URL/health"

   curl -s -X POST "$TUNNEL_URL/json" \
     -H 'Content-Type: application/json' \
     -d '{"name":"Tunnel","body":"It works"}'
   ```

5. Stop the app with `Ctrl-C`; the server logs `shutting down gracefully` and
   exits cleanly.

### Troubleshooting

- **`502 Bad Gateway` / `Unable to reach the origin service`** and the
  cloudflared log shows
  `error="dial tcp [::1]:3333: connect: connection refused"`:
  the app is not running (or is on a different port). The tunnel itself is fine —
  start the app, re-run the `/health` preflight check, then retry the request.
- **Port mismatch**: make sure the tunnel points at `http://localhost:3333`,
  which matches `addr` in `main.go`.
- **No `trycloudflare.com` URL / `failed to unmarshal quick Tunnel`**: this is a
  transient Cloudflare-side issue. Wait and retry, optionally forcing a
  protocol:

  ```bash
  cloudflared tunnel --protocol quic --url http://localhost:3333
  ```

- **URL stopped working**: the tunnel only lives while the `cloudflared` process
  is running. If you `Ctrl-C` it, the URL dies and the next run gives a new one.

### Security note

The Quick Tunnel URL is publicly reachable and has no authentication. Use it only
for short-lived testing and never expose secrets or sensitive data. For anything
permanent, use a named tunnel or real hosting.
