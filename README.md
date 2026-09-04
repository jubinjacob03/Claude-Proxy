# Claude-Proxy

Licensed Claude proxy system with three parts:
- [claude-proxy](file:///d:/My-Projects/Claude-Proxy/main.go) — Windows client proxy that activates once against the relay and then serves local Anthropic/OpenAI-compatible endpoints.
- [claude-relay](file:///d:/My-Projects/Claude-Proxy/cmd/claude-relay/main.go) — hosted relay that owns pooled upstream keys, validates licences, and meters spend.
- [web](file:///d:/My-Projects/Claude-Proxy/web/README.md) — admin panel for minting licences, managing the key pool, and reviewing usage.

## Local client setup

```powershell
.\make.ps1 run
```

Or build manually:

```powershell
go build -o claude-proxy.exe .
go build -o claude-tray.exe ./cmd/claude-tray
Copy-Item .env.example .env
```

Set these client values in `.env`:
- `RELAY_BASE_URL` — hosted relay base URL
- `LICENSE_KEY` — licence key issued by the admin panel
- `HOST` / `PORT` — local proxy bind address, default `127.0.0.1:3001`
- `AUTH_TOKEN` — optional local client auth token

The first successful launch activates the machine and writes `license.json` next to the executable. The tray app can show licence status from that cached activation.

## Hosted relay setup

```powershell
go build -o claude-relay ./cmd/claude-relay
set RELAY_TOKEN_SECRET=replace-me
set RELAY_DB_KEY=replace-me
set RELAY_ADMIN_TOKEN=replace-me
claude-relay add-key claude sk-upstream-key 100 primary
claude-relay
```

Required relay env:
- `RELAY_TOKEN_SECRET`
- `RELAY_DB_KEY`
- `RELAY_ADMIN_TOKEN`
- `UPSTREAM_BASE_URL`

## Admin panel setup

```powershell
cd web
npm install
Copy-Item .env.example .env
npm run hash-password -- "your-password"
npm run build
npm run start
```

Set these values in `web/.env`:
- `RELAY_BASE_URL`
- `RELAY_ADMIN_TOKEN`
- `ADMIN_USER`
- `ADMIN_PASSWORD_HASH`
- `SESSION_SECRET`

## Docker deployment

The repository now ships a two-service stack:
- [Dockerfile](file:///d:/My-Projects/Claude-Proxy/Dockerfile) for the relay
- [web/Dockerfile](file:///d:/My-Projects/Claude-Proxy/web/Dockerfile) for the admin panel
- [docker-compose.yml](file:///d:/My-Projects/Claude-Proxy/docker-compose.yml) for relay + web

Current public deployment:
- Web admin panel: `http://68.233.112.166:3005`
- Relay base URL: `http://68.233.112.166:43219`
- Local client proxy default bind: `127.0.0.1:3001`

Example:

```bash
RELAY_TOKEN_SECRET=...
RELAY_DB_KEY=...
RELAY_ADMIN_TOKEN=...
UPSTREAM_BASE_URL=https://gorouter.app
ADMIN_USER=admin
ADMIN_PASSWORD_HASH=...
SESSION_SECRET=...
docker compose up -d --build
```

## What the admin panel can do

- Mint licences and show keys once
- Pause, resume, delete, reset HWID, and set quota
- Inspect a licence detail page with recent usage
- Add, disable, enable, delete, top up, and rotate pooled upstream keys
- Review recent metered requests

## Verification

```powershell
go test ./... -count=1
cd web
npm run build
```
