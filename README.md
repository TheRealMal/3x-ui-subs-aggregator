# 3X-UI Subs Aggregator

A lightweight Go service that aggregates subscription configs from multiple [3X-UI](https://github.com/MHSanaei/3x-ui) panels into a single unified subscription endpoint.

## Features

- **Subscription merging** — fetches configs from all configured panels concurrently, merges and returns as a single base64 subscription
- **Admin API** — look up subscription URL by user email, create clients across all panels at once
- **Pure Go** — built with `net/http`, no external frameworks
- **Background operation** — shell script for start/stop/status management

## API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/sub/{subId}` | Merged subscription from all panels |
| `GET` | `/admin/sub-url/{email}` | Get unified subscription URL by email |
| `POST` | `/admin/clients` | Create client across all panels |
| `GET` | `/health` | Health check |

### Create Client Request

```json
{
  "email": "user@example.com",
  "inboundIds": [1, 2, 3],
  "totalGB": 0,
  "expiryTime": 0,
  "limitIp": 2
}
```

## Setup

1. Copy and edit the config:
   ```bash
   cp configs/config.example.yaml configs/config.yaml
   ```

2. Fill in your panel details in `configs/config.yaml`:
   ```yaml
   server:
     port: 8080

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
