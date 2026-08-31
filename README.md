# Claude-Proxy

A single-binary **bridge** that puts [GoRouter](https://gorouter.app)'s
per-request Claude models behind the exact APIs your tools already speak — the
**Anthropic Messages API** and the **OpenAI Chat Completions API** — with a web
dashboard, live config, admin endpoints, and a one-command Claude Code launcher.

The architecture follows the [GLM-Free-API](https://github.com/izaart95-jpg/GLM-Free-API)
Go "bridge" layout: a thin `main.go` over a single `internal/bridge` core package
split by concern (`run`, `config`, `types`, `util`, `middleware`, `handlers`,
`anthropic`, `format`, `models`, `admin`), management/admin endpoints, client and
request telemetry, aligned `HOST`/`PORT`/`AUTH_TOKEN` config, graceful shutdown,
and Docker deployment. The provider layer targets GoRouter/Claude instead of
Z.AI/GLM.

Point **Trae** or **Claude Code** at `http://127.0.0.1:3001`, drop in your
GoRouter key, and Claude just works.

---

## What it does

- **Dual protocol** — OpenAI `/v1/chat/completions` and Anthropic `/v1/messages`
  on one server; streaming (SSE, keep-alive pings) and non-streaming.
- **Native Anthropic passthrough (default)** — GoRouter serves real Claude models
  on an Anthropic endpoint, so requests forward losslessly (tools, `cache_control`,
  thinking, betas intact). Flip `UPSTREAM_FORMAT=openai` to translate instead.
- **Full translation** — Anthropic ⇄ OpenAI for messages, system prompts, tools,
  images, and reasoning, in both directions.
- **Web dashboard** at `/` — settings, live model catalog, and a test prompt.
- **Live config** — `GET/POST /config` change upstream, key, model, and toggles
  at runtime, optionally persisted to `.env`.
- **Management endpoints** — `/health`, `/status`, `/admin/stats`,
  `/admin/clients` with per-client request telemetry.
- **`claude-proxy claude`** — starts the bridge and launches Claude Code wired to
  it.

### Adapted for GoRouter (vs GLM-Free-API)

The architecture is mirrored; the Z.AI-specific machinery is intentionally
dropped because GoRouter serves real Claude models over a proper API:

| GLM-Free-API (Z.AI)                        | Claude-Proxy (GoRouter)                       |
| ------------------------------------------ | --------------------------------------------- |
| Guest/JWT session init, captcha, signature | Plain API-key auth to GoRouter                |
| Throwaway session pool + token collector   | Stateless — no sessions to manage             |
| Agent-mode role/tool folding shims         | Native tool_use / tools pass straight through |
| Vision upload to Z.AI file endpoint        | Images pass through in the request            |

---

## The cost model

GoRouter bills **per request**, not per token: `cost = base_price × group_ratio`.
`claude-opus-4-8` at `$0.2` (group ratio `1x`) is `$0.2` per call regardless of
tokens. Agentic loops cost per step, so request count is what matters.

---

## Quick start

Requires the [Go toolchain](https://go.dev/dl/) (1.23+).

```powershell
.\make.ps1 build          # -> claude-proxy.exe
Copy-Item .env.example .env
# edit .env: set UPSTREAM_API_KEY to your GoRouter token
.\claude-proxy.exe        # serve (default)
```

Open **http://127.0.0.1:3001/** for the dashboard, or:

```powershell
curl.exe http://127.0.0.1:3001/health
```

### Docker

```bash
UPSTREAM_API_KEY=sk-your-key AUTH_TOKEN=choose-a-token docker compose up -d
curl http://127.0.0.1:3001/health
```

The image binds `0.0.0.0` inside the container. **Set `AUTH_TOKEN`** whenever the
port is reachable beyond localhost.

---

## CLI

```
claude-proxy [command] [flags]

  serve            Run the bridge + dashboard (default)
  claude [args]    Start the bridge and launch Claude Code wired to it
  env              Print shell exports to point Claude Code at the bridge
  status           Check whether the bridge is running
  version | help

Serve flags: --port <n>  --host <addr>  --auth-token <key>  --verbose
```

---

## Wiring up Trae

Add a custom model — either provider type works:

**Anthropic-compatible (recommended):** Base URL `http://127.0.0.1:3001`,
model `claude-opus-4-8`.

**OpenAI-compatible:** Base URL `http://127.0.0.1:3001/v1`, model `claude-opus-4-8`.

API Key: your `AUTH_TOKEN` if set, otherwise any value (the bridge uses
`UPSTREAM_API_KEY` upstream). The dashboard's "Connect your tools" panel prints
exact snippets.

## Wiring up Claude Code

```powershell
.\claude-proxy.exe claude          # one command: bridge + Claude Code
```

Or manually:

```powershell
$env:ANTHROPIC_BASE_URL   = "http://127.0.0.1:3001"
$env:ANTHROPIC_AUTH_TOKEN = "your-auth-token-or-anything"
$env:ANTHROPIC_MODEL      = "claude-opus-4-8"
$env:CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY = "1"
claude
```

The bridge honors the Claude Code gateway contract: streams without buffering,
forwards `anthropic-version`/`anthropic-beta`, emits keep-alive pings, and serves
`/v1/models` and `/v1/messages/count_tokens`.

---

## Configuration

Set via `.env`, environment variables, the dashboard, or `serve` flags. Env vars
win at startup; the dashboard writes back to `.env`.

| Variable                   | Default                | Description                                                      |
| -------------------------- | ---------------------- | ---------------------------------------------------------------- |
| `HOST`                     | `127.0.0.1`            | Bind address.                                                    |
| `PORT`                     | `3001`                 | Port.                                                            |
| `AUTH_TOKEN`               | _(empty)_              | Client auth token (Bearer / x-api-key). Empty = localhost trust. |
| `UPSTREAM_BASE_URL`        | `https://gorouter.app` | Router base URL, no trailing `/v1`.                              |
| `UPSTREAM_FORMAT`          | `anthropic`            | `anthropic` (passthrough) or `openai` (translate).               |
| `UPSTREAM_API_KEY`         | _(empty)_              | GoRouter key. If empty, the client's key is forwarded.           |
| `DEFAULT_MODEL`            | `claude-opus-4-8`      | Model the CLI launcher and dashboard test box use.               |
| `MODEL_MAP`                | _(empty)_              | `from=to` pairs, `*` catch-all.                                  |
| `DEFAULT_MAX_TOKENS`       | `4096`                 | Fills Anthropic's required `max_tokens` for OpenAI clients.      |
| `STREAM_IDLE_PING_SECONDS` | `15`                   | Keep-alive ping cadence on the translating path.                 |
| `TIMEOUT`                  | `0`                    | Per-request timeout in **milliseconds**. `0` = none.             |
| `LOG_LEVEL`                | `info`                 | `debug`\|`info`\|`warn`\|`error`\|`off`.                         |
| `LOG_FORMAT`               | `text`                 | `text` or `json`.                                                |
| `LOG_BODIES`               | `false`                | Log request bodies at debug level.                               |

---

## Endpoints

| Method   | Path                        | Auth | Purpose                                    |
| -------- | --------------------------- | ---- | ------------------------------------------ |
| POST     | `/v1/messages`              | ✅   | Anthropic Messages API                     |
| POST     | `/v1/messages/count_tokens` | ✅   | Token counting (forwarded or estimated)    |
| POST     | `/v1/chat/completions`      | ✅   | OpenAI Chat Completions API                |
| GET      | `/v1/models`                | ✅   | Model list with architecture/modality      |
| GET      | `/`                         | ❌   | Web dashboard                              |
| GET/POST | `/config`                   | ❌   | Read / update live config (secrets masked) |
| GET      | `/models`                   | ✅   | Compact `{ models, currentModel }` shape   |
| GET/POST | `/features`                 | ✅   | Per-model request defaults (below)         |
| POST     | `/stop`                     | ✅   | Acknowledged no-op (client compatibility)  |
| GET      | `/health`, `/admin/health`  | ❌   | Status + validation                        |
| GET      | `/status`                   | ❌   | Live mode, uptime, request/client counts   |
| GET      | `/admin/stats`              | ❌   | Aggregate request/client stats             |
| GET      | `/admin/clients`            | ❌   | Per-client request telemetry               |

### `/features` — per-model defaults

Store request defaults per model. Overrides **only fill in fields the client
omitted**, so an explicit client value always wins:

```bash
# force thinking + a token budget on a thinking model
curl -X POST http://127.0.0.1:3001/features \
  -H 'Content-Type: application/json' \
  -d '{"model":"claude-opus-4-8-thinking","thinking":true,"thinking_budget_tokens":2048}'

# inspect what resolves for a model
curl "http://127.0.0.1:3001/features?model=claude-opus-4-8-thinking"
```

Keys: `thinking` (bool), `thinking_budget_tokens`, `max_tokens`, `temperature`,
`top_p`. `thinking` is only injected for models whose id advertises it (GoRouter
exposes explicit `-thinking` variants), so it can't 400 a non-thinking model.

`✅` requires `AUTH_TOKEN` only when one is configured.

---

## Project layout

```
Claude-Proxy/
├── main.go                     # thin entry: subcommand dispatch -> bridge
├── Dockerfile, docker-compose.yml
├── make.ps1, .env.example
└── internal/
    ├── bridge/                 # core package (GLM-Free-API-style split)
    │   ├── run.go              # NewServer, Handler, Start/Run, shutdown, banner
    │   ├── config.go           # Config + env/.env loading, Clone/Save
    │   ├── types.go            # Server state + client/request telemetry
    │   ├── util.go             # http client, sse/flush/json helpers
    │   ├── middleware.go       # CORS, auth, access log, request tracking
    │   ├── access.go           # one summary line per request
    │   ├── recover.go          # panic containment (handlers + goroutines)
    │   ├── handlers.go         # /v1/chat/completions
    │   ├── anthropic.go        # /v1/messages + count_tokens
    │   ├── format.go           # OpenAI/Anthropic error envelopes
    │   ├── models.go           # /v1/models + /models + discovery + architecture
    │   ├── features.go         # per-model feature resolution + /features, /stop
    │   ├── admin.go            # /health, /status, /admin/*
    │   ├── metrics.go          # /metrics (Prometheus text)
    │   ├── config_api.go       # GET/POST /config
    │   ├── upstream.go         # upstream request build + passthrough
    │   ├── dashboard.go        # embedded dashboard
    │   ├── features_test.go    # whitebox: features, model map, auth surface
    │   └── web/                # index.html, app.js, styles.css
    ├── ansi/                   # terminal colouring (NO_COLOR aware)
    ├── logx/                   # leveled logger (text/json), redaction
    ├── anthropic/ · openai/    # API types
    └── translate/              # Anthropic <-> OpenAI converters + streaming
        └── translate_test.go   # whitebox: translation + SSE state machines
tests/
└── integration_test.go         # blackbox: E2E HTTP against a mock GoRouter
```

Run the suite with `go test ./...` or `.\make.ps1 check` (gofmt + vet + tests).

Standard library only — no external dependencies.

## Security notes

- Binds `127.0.0.1` by default; it holds your GoRouter key. Set `AUTH_TOKEN` (and
  TLS) before exposing it beyond localhost.
- `AUTH_TOKEN` is compared in constant time; `/v1/*` requires it when set.
- **CORS is only granted to loopback origins.** Reflecting an arbitrary `Origin`
  would let any website you visit drive this proxy from your browser — including
  repointing `upstream_base_url` at their server and capturing your key. Remote
  origins get no CORS headers, so the browser blocks them.
- **`POST /config` and `POST /features` require `AUTH_TOKEN`** when one is set,
  because they can change where traffic goes. `GET` stays open so the dashboard
  loads; it only ever returns a masked key hint.
- `/admin/clients` shows key fingerprints (not keys) and IPs; fine for localhost.
- If you set `AUTH_TOKEN`, enter it in the dashboard's token field so the
  dashboard can save settings and call the API.

## License

MIT.
