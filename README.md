# 3X-UI Subscription Unifier

A lightweight Go service that aggregates subscription configs from multiple [3X-UI](https://github.com/MHSanaei/3x-ui) panels into a single unified subscription endpoint.

## Features

- **Subscription merging** — fetches configs from all configured panels concurrently, merges and returns as a single base64 subscription
- **Admin API** — list inbounds, create clients across all panels at once, look up unified subscription URL by user name
- **Protocol-aware** — automatically adapts credentials for vmess, vless, trojan, and shadowsocks inbounds
- **Pure Go** — built with `net/http`, no external frameworks
- **Graceful shutdown** — handles SIGINT/SIGTERM with a 10-second drain timeout

## Concepts

**Inbound** — a proxy listener configured on a 3X-UI panel. Each inbound has a protocol (vmess, vless, trojan, shadowsocks), a port, and a remark (human-readable name like `vless-reality`). Inbound IDs are scoped to their panel — the same numeric ID can exist on different panels. When creating a client, you specify target inbounds by **remark**, not by ID.

**Client name** — a string identifier (e.g. `therealmal-mac`) passed when creating a client. The subscription ID is deterministically derived from this name via SHA-256, so the same name always produces the same subscription URL. Within 3X-UI, each inbound receives a suffixed email (`name-1`, `name-2`, ...) to satisfy the per-panel uniqueness constraint, but the subscription URL depends only on the base name.

## API Endpoints

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| `GET` | `/sub/{subId}` | None | Merged subscription from all panels |
| `GET` | `/admin/inbounds` | `?secret=` | List all inbounds from all panels |
| `GET` | `/admin/sub-url/{name}` | `?secret=` | Get unified subscription URL by user name |
| `POST` | `/admin/clients` | `?secret=` | Create client across all panels |
| `GET` | `/health` | None | Health check |

Admin endpoints require the `secret` query parameter matching `admin_secret` from the config.

### List Inbounds

```
GET /admin/inbounds?secret=your-secret
```

Returns all inbounds from all configured panels with their IDs, remarks, protocols, ports, and enabled status. Use the **remark** values from this response as the `inbounds` parameter when creating a client.

### Create Client

```
POST /admin/clients?secret=your-secret
```

```json
{
  "name": "therealmal-mac",
  "inbounds": ["vless-reality", "vmess-ws"],
  "totalGB": 0,
  "expiryTime": 0,
  "limitIp": 2
}
```

- **`name`** (required) — client identifier, used to derive the subscription URL and as the base for per-inbound emails (`therealmal-mac-1`, `therealmal-mac-2`, ...)
- **`inbounds`** (optional) — list of inbound remarks to add the client to. If omitted or empty, the client is created on **all inbounds** across all panels
- **`totalGB`** — traffic quota in GB (0 = unlimited)
- **`expiryTime`** — expiration as unix timestamp in milliseconds (0 = never)
- **`limitIp`** — max concurrent connections (0 = unlimited)

The service generates a shared UUID for the client and adds it to every target inbound on every panel. For vmess/vless inbounds the UUID is used as the client ID; for trojan/shadowsocks it is used as the password.

### Get Subscription URL

```
GET /admin/sub-url/therealmal-mac?secret=your-secret
```

Returns the unified subscription URL that aggregates configs for this user from all panels.

## Setup

1. Copy and edit the config:
   ```bash
   cp configs/config.example.yaml configs/config.yaml
   ```

2. Fill in your panel details in `configs/config.yaml`:
   ```yaml
   server:
     port: 8080
     admin_secret: "change-me-to-a-strong-secret"

   panels:
     - name: "server-1"
       address: "https://panel1.example.com:2053"
       username: "admin"
       password: "your-password"
       sub_path: "/sub"
     - name: "server-2"
       address: "https://panel2.example.com:2053"
       username: "admin"
       password: "your-password"
       sub_path: "/sub"

   log:
     level: "info"
   ```

   At least 2 panels are required.

3. Build and start:
   ```bash
   ./scripts/unifier.sh start
   ```

## Management

```bash
./scripts/unifier.sh start    # Build (if needed) and start in background
./scripts/unifier.sh stop     # Graceful stop
./scripts/unifier.sh restart  # Stop + start
./scripts/unifier.sh status   # Check if running
./scripts/unifier.sh build    # Build binary only
```

## Requirements

- Go 1.24+
